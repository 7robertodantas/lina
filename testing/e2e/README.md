# End-to-end integration tests

These tests drive a **running** gateway + MQTT stack (provision, credit, device MQTT, consumptions). They live in their own Go module (`go.mod` in this directory), so run them **from `testing/e2e`** (or use `go -C` from the repo root).

Tests are behind the **`integration` build tag** and are skipped unless **`LINA_E2E=1`**.

## Run all integration tests (verbose)

From this directory:

```bash
cd testing/e2e

LINA_E2E=1 \
LINA_E2E_BASE_URL=http://192.168.0.170:8080 \
LINA_E2E_MQTT_HOST=192.168.0.170 \
LINA_E2E_MQTT_TLS_SKIP_VERIFY=1 \
go test -tags=integration -v ./...
```

From the repository root without changing directory:

```bash
LINA_E2E=1 \
LINA_E2E_BASE_URL=http://192.168.0.170:8080 \
LINA_E2E_MQTT_HOST=192.168.0.170 \
LINA_E2E_MQTT_TLS_SKIP_VERIFY=1 \
go test -C testing/e2e -tags=integration -v ./...
```

## Local stack (defaults)

If the gateway is on `http://127.0.0.1:8080` and MQTT TLS on `127.0.0.1:8883` with the default CA path (`../../infrastructure/certs/ca.crt` relative to this module):

```bash
cd testing/e2e
LINA_E2E=1 go test -tags=integration -v ./...
```

## Run a single test

```bash
cd testing/e2e
LINA_E2E=1 LINA_E2E_BASE_URL=http://192.168.0.170:8080 \
LINA_E2E_MQTT_HOST=192.168.0.170 LINA_E2E_MQTT_TLS_SKIP_VERIFY=1 \
go test -tags=integration -v -run TestSmokeProvisionConnectUsage ./...
```

## Extra environment variables

| Variable | Purpose |
|----------|---------|
| `LINA_E2E_SERVICE_TOKEN` | `X-Service-Token` for ledger/credit/consumption APIs (default `dev-token`) |
| `LINA_E2E_MQTT_TLS_PORT` | MQTT TLS port (default `8883`) |
| `LINA_E2E_MQTT_CA` | Path to broker CA PEM |
| `LINA_E2E_MQTT_TLS_SERVER_NAME` | TLS ServerName when the cert hostname does not match |
| `LINA_E2E_DEVICE_TRACE=1` | Log all device `OnLog` lines (noisy) |

See `harness.go` (`LoadEnv` / `Env` documentation) for the full list. Scenario coverage is summarized in `TEST_SCENARIOS.md`.

## Do not run a single `*_test.go` file from the repo root

`go test path/to/lifecycle_integration_test.go` from the monorepo root does not use this module’s `go.mod` and will fail to resolve imports. Prefer `go test` on the package: `./...` or `.` from `testing/e2e`, or `go test -C testing/e2e ...` from the root.
