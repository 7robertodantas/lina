# Execution Environment

Running Architecture in Edge Node and External Machine

## Network

Edge Node at 192.168.0.170
External Machine at 192.168.0.166

## Running the infrastructure on edge node 

```bash
docker compose -f deployment/docker-compose.evaluation.edge.yml up -d
```

That runs:
- caddy (192.168.0.170:8080)
- mosquitto (192.168.0.170:8883)
- redis
- device service
- ledger service
- consumption service
- lightning service
- redis exporter (prometheus metrics) 
- cadvisor exporter (prometheus metrics)
- node exporter (prometheus metrics)

Then, the edge was configured to collect the service logs to a file so that we can observe them later.

```bash
docker compose -f deployment/docker-compose.evaluation.edge.yml logs -f -t --no-color device ledger lightning consumption |& tee functional-evaluation-edge-node.log
```

## Running the LND node on External Machine

On Polar Lightning software, a new network was created with a bitcoin core in regtest mode.

Two LND nodes (v0.19.1-beta), alice and bob, connected to the single bitcoin core (v29.0)

6 payment channels created from bob to alice each with 15,000,000 sats of capacity to have enoguh liquditiy for both functional tests and load testing.

LND Node A (Alice / LINA): 192.168.0.166:10001
LND Node B (User / Payer): 192.168.0.166:10002


## Running the infrastructure on external machine

```bash
docker compose -f deployment/docker-compose.evaluation.external.yml up -d
```

That runs:
- autopay service
- smart meter device
- http devices
- prometheus server
- grafana 

For functional tests the auto pay was disabled and the payment was made manually by copying the QR Code and pasting into the Polar Lightning software via the LND Node B to pay the invoice created by the edge node through the LND Node A.

The http devices was not used in functional tests, instead, the smartmeter was used in a fixed usage report mode.

The external machine was configured to also collect smart meter logs.

```bash
docker compose -f deployment/docker-compose.evaluation.external.yml logs -f -t --no-color smartmeter |& tee functional-evaluation-external-machine.log
```

## Device Setup

A device was provisioned via the Northbound Interface.

```shell
curl --location '192.168.0.170:8080/devices' \
--header 'Content-Type: application/json' \
--data '{
    "device_id": "smart-meter-001",
    "device_secret": "smart-meter-001_password",
    "measurement_unit": "kWh",
    "unit_price_msat": 100000,
    "reporting_strategy": "interval",
    "reporting_interval": 1,
    "heartbeat_interval": 60,
    "authorize_request_msat": 1000,
    "timestamp": "2026-01-07T15:04:59.351Z"
}'
```

For the functional test, 10000 msats is going to be added to the device balance.

The device was configured to emit usage reports at a fixed frequency of 1 report/s at a unit price of 100000 msat and to request authorization slots of 1000 msat. 

The smartmeter was also configured to emit a fixed 0.0010 kWh/s upon any appliance turned on.

Considering that each kWh unit is set to cost 100.000 msat (100 sats), each usage report is expected to consume 100 msat.

Each authorization request reserves 1.000 msats, so each authorization is expected to complete after 10 usage reports. The smart meter is expected therefore to perform 10 authorization requests until the device balance is reached 0.

In summary, the device should:
- Create invoice for 10 sats (10.000 msats).
- Detect invoice payment and new device balance (10.000 msats)
- Submit 10 authorization requests in total (each 1.000 msat)
    - Within each authorization submit 10 usage reports of 0.0010 kWh (100 msat).
- Exausth the device balance after 10 authorizations iterations (or 100 usage reports in total).

It is expected:
- 1 Invoice created and paid.
- 10 authorization requests.
- 100 reports of 0.0010 kWh (100 msat)
- Device balance 0 at the end.
- STOP command received at the end due to insufficient funds.

First Iteration:

    Initial Balance: 10.000 msat.
    Balance: 9.000 msat.

Last Iteration (10 authorizations)

    Previous Balance: 1.000 msat.
    New Balance: 0 msat.

# Evaluation and Observations

## Scenario 1: Funding a Device via Lightning

## Scenario 2: Authorization and Prepaid Allowance

## Scenario 3: Usage Reporting and Debit Application

## Scenario 4: Allowance Exhaustion and Device Interruption

