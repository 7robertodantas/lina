# LINA / prototype — end-to-end test scenarios

This document lists **end-to-end (E2E) test scenarios** for the **Lightning Integrated Node Architecture (LINA)** and its prototype implementation (Chapters 4–5, appendix technical references). It is intended as input for an AI agent (or humans) to **generate automated tests** against the real stack: **Mosquitto (MQTT/TLS)**, **Device / Consumption / Ledger / Lightning services**, **Redis Streams**, **Caddy gateway (northbound)**, **LND (e.g. regtest / Polar)**.

**Scope notes**

- **In scope:** Full paths across southbound (device), east–west (streams + gRPC effects observable indirectly), cloud (LND), and northbound (admin REST via gateway).
- **Prototype limitation:** Usage reporting is effectively **interval-oriented** in the implementation; **delta** and **total** strategies are **architecture-specified** — include them as **future / contract tests** once implemented.
- **Out of explicit E2E scope (per thesis threat model):** Compromise of edge node, internal transport MITM, direct DB tampering, compromised LND — do not assert “attack prevention” beyond documented behaviors (e.g. MQTT ACL, admin auth).

---

## 1. Conventions for writing tests

| Item | Guidance |
|------|----------|
| **Device identity** | Unique `device_id`; secret provisioned with device (Dynamic Security: username = device id, password = secret). |
| **MQTT topics** | Under `devices/{id}/…` — heartbeat, usage, request/authorize, request/invoice; server publishes config (retained), response/*, balance (retained), control, events/invoice. |
| **Admin API** | Through Caddy; paths as in prototype: e.g. `POST /api/v1/devices`, `GET /api/v1/devices`, `GET /api/v1/devices/{id}/balance`, entries, authorizations, consumptions, `GET /api/v1/lnd/info`, `GET /api/v1/lnd/wallet`. Include **chapter mapping** variants: `/devices`, `/devices/*/balance` if tests target gateway routes rather than raw service ports. |
| **Assertions** | Prefer **observable contracts**: MQTT payloads, REST JSON, ledger/consumption rows, invoice lifecycle; optional **Jaeger** trace presence for critical flows. |
| **Async** | Use timeouts and polling; many paths are **eventually consistent** (balance topic, settlement → `DeviceCredited`). |

---

## 2. Environment matrix (permutations)

Run critical scenarios under each combination where feasible:

| Dimension | Variants |
|-----------|----------|
| **LND topology** | Single LND; two nodes (invoice on Alice, pay from Bob) as in Polar; reconnect Lightning Service mid-test. |
| **Device count** | 1 device; 2+ devices in parallel (different namespaces). |
| **Connectivity** | Stable; **flaky MQTT** (drop/reorder/delay publishes); **service restart** (one container at a time: Device, Consumption, Ledger, Lightning, Redis, Mosquitto). |
| **Persistence** | Fresh volumes; **restart with existing SQLite + Redis** after partial flows. |
| **Admin auth** | Valid token; **missing token**; **wrong token** (expect 401/403 per implementation). |
| **MQTT auth** | Valid device credentials; **wrong secret**; **wrong device id** (cannot subscribe/publish peer topics). |

---

## 3. Northbound (administrative) E2E scenarios

### 3.1 Device provisioning

| ID | Scenario | Steps (high level) | Expected |
|----|----------|-------------------|----------|
| NB-001 | **Provision single device** | `POST /api/v1/devices` with full config (measurement unit, `unit_price_msat`, reporting fields, `authorize_request_msat`, etc.). | 201/200; device appears in `GET /api/v1/devices`; `GET /api/v1/devices/{id}` returns stored config; MQTT user exists with ACL for that `id`. |
| NB-002 | **Idempotent upsert** | Same `device_id` posted twice with same then **different** config. | Second call updates or rejects per API contract; MQTT ACL still consistent. |
| NB-003 | **Batch provision** | `POST /api/v1/devices/batch` with N devices (N = 1, 5, max supported). | All devices listable; no cross-device topic bleed. |
| NB-004 | **Invalid payload** | Omit required fields; negative prices; empty `device_id`. | 4xx; no partial MQTT user creation (if transactional). |
| NB-005 | **Provision then connect** | After NB-001, device simulator connects with issued credentials. | Connection succeeds; can subscribe to own `config` / `control`. |

### 3.2 Read-only inspection (consistency with southbound flows)

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| NB-010 | **Balance matches ledger** | After known credits/debits, compare `GET .../balance` vs sum logic from `GET .../entries`. | Coherent `balance_msat` / entry history (newest-first ordering per API). |
| NB-011 | **Authorizations list** | After auth create / complete / expire, fetch `GET .../authorizations`. | Status transitions reflected; `request_id` visible where applicable. |
| NB-012 | **Consumptions list** | Publish usage; poll `GET .../consumptions?limit=…` (limits 1, 50, 100, invalid limit). | Records appear with expected debit; pagination respected. |
| NB-013 | **Invoices** | After funding flow, if `.../invoices` exists (gateway table), list invoices for device. | Correlates with settled invoice idempotency (see FUND scenarios). |
| NB-014 | **LND info / wallet** | `GET /api/v1/lnd/info`, `GET /api/v1/lnd/wallet`. | Non-error when LND up; sensible regtest values; error path when LND down. |

### 3.3 Gateway security

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| NB-020 | **No auth header** | Call admin endpoints without token. | Rejected. |
| NB-021 | **Invalid token** | Wrong static token. | Rejected; no data leak in body. |

---

## 4. Southbound — device lifecycle & configuration

### 4.1 Connection & heartbeat

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| SB-001 | **Connect + subscribe** | Device connects TLS; subscribes to config, response/*, balance, control, events/invoice. | Subscriptions succeed under ACL. |
| SB-002 | **Heartbeat cadence** | Publish heartbeat every T seconds (match and **violate** suggested `heartbeat_interval`). | Edge accepts messages; (if implemented) offline detection — document current behavior. |
| SB-003 | **Heartbeat payload** | Valid JSON with `device_id`, `status` ONLINE/OFFLINE, `timestamp`. | No crash; optional persistence. |
| SB-004 | **Malformed heartbeat** | Invalid JSON / wrong schema. | Logged/discarded; broker still OK. |

### 4.2 Configuration (retained)

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| SB-010 | **Receive retained config** | On subscribe, receive last config for device. | Fields align with provisioned device: `unit_price_msat`, `reporting_strategy`, intervals, `authorize_request_msat`. |
| SB-011 | **UPDATE_CONFIG command** | Edge sends `UPDATE_CONFIG` on `control` (if exposed in impl). | Device reloads; subsequent usage uses new pricing (verify via consumption debit). |
| SB-012 | **Price change mid-flight** | Change `unit_price_msat` between two usage reports. | Consumption uses **enriched** price at time of Device Service handling (verify via entries). |

### 4.3 Control commands

Exercise each command from architecture (`STOP`, `PAUSE`, `RESUME`, `REBOOT`, `PING`, `UPDATE_CONFIG`, `AUTHORIZATION`) as **observable E2E** where the simulator or test stub implements reactions:

| ID | Command | Variations |
|----|---------|------------|
| SB-020 | **STOP** | Reasons: `OUT_OF_FUNDS`, generic; device stops publishing usage until new auth. |
| SB-021 | **RESUME** | After STOP/PAUSE; usage resumes. |
| SB-022 | **PAUSE** | State preserved vs STOP (per device semantics). |
| SB-023 | **PING** | Device emits heartbeat in response. |
| SB-024 | **AUTHORIZATION** | Sent when auth completed/expired; device requests new auth. |
| SB-025 | **REBOOT** | If implemented in simulator — reconnect cycle. |
| SB-026 | **Unknown command** | Forward-compatible handling (ignore vs error). |

---

## 5. Authorization (prepaid hold) E2E scenarios

### 5.1 Happy paths

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| AUTH-001 | **First authorization** | Device publishes `request/authorize` with `request_msat` ≤ available balance. | `response/authorize`: **GRANTED**; `authorization_id`, `granted_msat`, `remaining_msat`, `expires_at`; balance hold reflected (available vs reserved if exposed); **RESUME** if applicable. |
| AUTH-002 | **Active idempotency** | Same `request_id` while authorization still active. | **ACTIVE** response; same auth id; reason `ALREADY_ACTIVE` if present. |
| AUTH-003 | **New request after completion** | After auth fully consumed, new `request_id`. | New **GRANTED** if funds suffice. |
| AUTH-004 | **New request after expiry** | Wait until `expires_at` (or force clock); request new auth. | Expired path issues **AuthorizationExpired** internally; device gets **AUTHORIZATION** command; new grant after re-request. |

### 5.2 Rejections & errors

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| AUTH-010 | **Insufficient funds** | `request_msat` > balance. | **REJECTED**, `INSUFFICIENT_FUNDS`, `available_msat` correct; **STOP** (per device service pseudocode). |
| AUTH-011 | **Ledger gRPC failure** | Simulate Ledger down during authorize. | Device gets error path: auth error + **STOP** (pseudocode). |
| AUTH-012 | **Malformed authorize payload** | Missing `request_id` / invalid types. | No partial ledger state; safe failure. |

### 5.3 Hold & balance semantics

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| AUTH-020 | **Hold reduces spendable balance** | Compare `GET .../balance` (or balance topic) before/after grant. | Available decreases by hold amount; entries show `AUTHORIZATION_HOLD` (or equivalent reason). |
| AUTH-021 | **Multiple sequential holds** | Exhaust one hold; grant another; repeat. | Ledger always single active auth per device (per design); no double spend. |

---

## 6. Funding & Lightning invoice E2E scenarios

### 6.1 Invoice creation

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| FUND-001 | **Create invoice** | Device publishes `request/invoice` with `amount_msat`, `request_id`, `reason`. | `response/invoice`: **CREATED**, `bolt11`, `invoice_id`, `expires_at`. |
| FUND-002 | **Invalid invoice request** | Zero/negative amount; missing `device_id`. | Error path from Lightning Service; device notified (per impl). |
| FUND-003 | **LND unavailable** | Stop LND or break creds; request invoice. | Graceful error; no phantom credit. |

### 6.2 Settlement & expiry

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| FUND-010 | **Pay invoice** | Pay BOLT11 from second node / tooling. | `events/invoice` (or equivalent) **SETTLED**; `GET .../balance` increases; ledger **LIGHTNING_INVOICE_SETTLED** credit; **DeviceCredited** leads to balance publish. |
| FUND-011 | **Idempotent settlement** | Duplicate `InvoiceSettled` delivery (replay stream message / restart consumer). | **Single** credit applied; idempotency by `invoice_id`. |
| FUND-012 | **Expire unpaid invoice** | Wait or shorten expiry in test harness. | **EXPIRED** / **CANCELED** mapping; **no** balance credit. |
| FUND-013 | **Pay after device reconnect** | Disconnect device; settle; reconnect. | Retained balance or event delivery catches device up. |

### 6.3 Full journeys combining AUTH + FUND

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| FUND-020 | **Reject → fund → authorize** | AUTH rejected → invoice → pay → AUTH granted. | End state: operations resume. |
| FUND-021 | **Partial balance** | Request invoice for amount larger than typical top-up; pay exact. | Balance matches paid msat. |

---

## 7. Usage reporting, consumption, and ledger debits

### 7.1 Basic usage pipeline

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| USE-001 | **Single usage report** | With active auth, publish one `usage` message (valid `report_id`, measure, unit). | `DeviceUsageReported` → `DeviceConsumptionRecorded` → ledger debit; `GET .../consumptions` has row. |
| USE-002 | **Many reports** | Series of reports with **unique** `report_id`s. | Monotonic balance decrease; authorizations `remaining_msat` decreases. |

### 7.2 Idempotency & duplicates

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| USE-010 | **Duplicate report_id** | Send same usage payload twice. | **One** consumption debit; second ignored at Consumption Service. |
| USE-011 | **Retry after timeout** | Client retries same `report_id` after disconnect. | Still idempotent. |

### 7.3 Rounding & minimum debit (1 msat)

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| USE-020 | **Sub-msat fractional** | Choose `measure * unit_price_msat` < 1 (e.g. tiny measure or low price). | `debit_msat` rounds to **1** (per appendix pseudocode: ceil + minimum 1). |
| USE-021 | **Exact integer msat** | Values yielding integer msat. | Debit equals that integer. |
| USE-022 | **Large measure** | Stress large float / precision boundary. | No panic; deterministic rounding; ledger still consistent. |

### 7.4 Authorization boundary & overflow (architecture-critical)

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| USE-030 | **Debit within remaining** | Report cost < remaining auth. | **AuthorizationDebited**; remaining updates. |
| USE-031 | **Debit exceeds remaining** | Single report priced > remaining auth allowance. | Partial from auth; **overflow** debited from balance (`AUTHORIZATION_OVERFLOW`); auth **completed**; device must re-authorize. |
| USE-032 | **Exhaust via many small reports** | Sum of reports consumes auth exactly to zero. | **AuthorizationCompleted**; **AUTHORIZATION** command to device. |

### 7.5 Consumption without active authorization

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| USE-040 | **Usage after auth expired** | Let auth expire; still publish usage. | **AuthorizationDebitFailed** (`NO_ACTIVE_AUTHORIZATION`); messages remain in stream pending retry until new auth (per architecture buffering). After new auth, **eventually** processed. |
| USE-041 | **Usage before first auth** | Publish usage with zero auth. | Same failure path; no silent drop of financial meaning (verify eventual consistency after auth). |

### 7.6 Outbox / publish failure (Consumption Service)

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| USE-050 | **Redis down during publish** | Fail Redis briefly after DB commit in Consumption Service. | Outbox retry / periodic publisher eventually publishes; **no double debit** at ledger (`report_id` idempotency). |
| USE-051 | **Consumption Service restart** | Restart mid-stream processing. | Pending Redis messages redelivered; processing remains correct. |

### 7.7 Reporting strategies (contract / future)

| ID | Scenario | Notes |
|----|----------|-------|
| USE-F01 | **interval** | Primary prototype path — full matrix of intervals vs config. |
| USE-F02 | **delta** | Architecture only: two deltas in sequence; reconnect with persisted last ack. |
| USE-F03 | **total** | Architecture only: non-monotonic total rejected or handled per spec; gap detection. |

---

## 8. Authorization completion, stop/resume, and device behavior

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| STOP-001 | **Completed → AUTHORIZATION command** | Consume auth fully. | Control **AUTHORIZATION**; device requests new auth. |
| STOP-002 | **Insufficient funds after completion** | Balance zero; new auth rejected. | **STOP** with reason (e.g. out of funds). |
| STOP-003 | **Sufficient funds after completion** | Balance > 0; new auth granted. | **RESUME** (per architecture stop flow). |
| STOP-004 | **Reports while waiting for new auth** | Device keeps sending usage. | Buffered in `event.consumption` / not applied until new auth; then catch-up. |
| STOP-005 | **AuthorizationExpired with remaining** | Expire with unused hold. | **AUTHORIZATION_EXPIRED** credit of remainder (per ledger pseudocode); balance increases; **AuthorizationExpired** event drives control. |

---

## 9. Multi-device isolation & concurrency

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| ISO-001 | **Two devices parallel** | Both provisioned; simultaneous usage + funding. | No cross-device balance; MQTT ACL denies wrong topics. |
| ISO-002 | **Topic spoof attempt** | Device A tries publish to `devices/B/usage` (should fail at broker). | Failure / no ledger effect on B. |
| ISO-003 | **Concurrent authorizations** | Rapid duplicate `request_id` from same device (parallel clients). | Single outcome; no double hold. |
| ISO-004 | **Concurrent usage same device** | Parallel publishers same `report_id` vs distinct ids. | Idempotency vs cumulative correctness. |

---

## 10. Durability & restart scenarios

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| DUR-001 | **Ledger restart** | Restart Ledger during consumption processing. | No duplicate debits; streams reconcile. |
| DUR-002 | **Device Service restart** | Restart during active MQTT traffic. | Reconnect; retained config/balance; no duplicate invoice on same `request_id` (if idempotent at Lightning/Ledger boundary — specify per impl). |
| DUR-003 | **Lightning Service restart** | Restart during invoice wait. | Subscription resumes; settlement still detected. |
| DUR-004 | **Full stack restart** | `docker compose down/up` with volumes. | Persistent state restored; devices can continue or re-provision cleanly. |

---

## 11. Observability (optional E2E hooks)

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| OBS-001 | **Trace spans invoice path** | Trigger funding; query Jaeger for trace across Device → Lightning → Ledger. | Connected spans; trace_id in logs (if enabled). |
| OBS-002 | **Metrics counters** | Scrape Prometheus endpoints after N usage reports. | Counters increase (consumption count, debit latency histograms, etc.). |

---

## 12. Negative, abuse, and schema scenarios

| ID | Scenario | Steps | Expected |
|----|----------|-------|----------|
| NEG-001 | **Unknown device usage** | Publish to topic matching unprovisioned id (if ACL allows — may not). | Device Service ignores (`conf == NULL` pseudocode). |
| NEG-002 | **Protobuf/JSON schema mismatch** | Send valid JSON with extra fields / wrong types. | Discarded safely. |
| NEG-003 | **Oversized payload** | Large strings in memo/pathological BOLT11 mock. | Bounded errors. |
| NEG-004 | **Clock skew** | Timestamps in future/past. | Behavior documented; no crash. |

---

## 13. Regression suites (suggested groupings)

1. **Smoke:** NB-001, SB-001, AUTH-001, USE-001, FUND-001 → FUND-010 (minimal regtest pay).  
2. **Money-critical:** AUTH-010, USE-010, USE-020, USE-031, FUND-011, STOP-002, STOP-005.  
3. **Async pipeline:** USE-040, USE-050, DUR-001.  
4. **Security:** NB-020, NB-021, ISO-002.  
5. **Scale / soak:** ISO-001 + high-frequency USE-002 for sustained period (finds Redis pending, SQLite locks).  

---

## 14. Checklist for test implementers (AI agent)

- [ ] Use **real** MQTT TLS + Dynamic Security users from provisioning.  
- [ ] Align **topic paths** and **REST paths** with **Caddy** routing vs direct service ports.  
- [ ] Poll until **eventual consistency** with explicit timeouts.  
- [ ] For every financial assertion, cross-check **ledger entries** + **authorizations** + **consumptions**.  
- [ ] Tag tests as **prototype** vs **architecture-future** (delta/total, PAUSE semantics).  
- [ ] Record **LND** version/regtest assumptions in test README or env fixtures.  

---

## 15. Traceability matrix (requirements → scenario IDs)

| Requirement (Chapter 4) | Primary scenario IDs |
|-------------------------|----------------------|
| Device initialization | NB-001, SB-010, AUTH-001 |
| Heartbeat | SB-002, SB-003 |
| Balance + notification | AUTH-020, FUND-010, NB-010 |
| User UI / QR (simulator) | FUND-001, FUND-010 |
| Funding via LN | FUND-*, NB-014 |
| Authorization hold | AUTH-*, STOP-* |
| Usage + debit | USE-* |
| Exhaustion / interruption | STOP-001–004, USE-031 |
| Config & control | SB-011–012, SB-020–026 |
| Administrative inspection | NB-010–014 |
| Intermittent connectivity / durability | USE-040, USE-050, §10 |
| Idempotency / duplicate protection | USE-010, AUTH-002, FUND-011 |
| Per-device namespace / ACL | ISO-001, ISO-002 |

---

*Generated from: `Capitulos/Capitulo4.tex`, `Capitulos/Capitulo5.tex`, `Includes/appendix/techref/*`, and related appendix MQTT/event definitions.*
