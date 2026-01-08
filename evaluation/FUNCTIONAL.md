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

## Scenario 1: Device Not Authorized

We attempt to connect the device prior to the provisioning and the mqtt has rejected.

```
smartmeter  | 2026-01-07T18:16:22.728976554Z timestamp=2026-01-07T18:16:22.728632054Z level=info service=smart-meter-core message="Connecting to MQTT broker..." device_id=smart-meter-001
smartmeter  | 2026-01-07T18:16:22.898076054Z timestamp=2026-01-07T18:16:22.897365721Z level=error service=smart-meter-core message="MQTT connection failed: not Authorized" device_id=smart-meter-001
smartmeter  | 2026-01-07T18:16:22.898141888Z timestamp=2026-01-07T18:16:22.897439388Z level=error service=smart-meter-core message="MQTT credentials rejected: shutting down" device_id=smart-meter-001
smartmeter  | 2026-01-07T18:16:22.898182346Z timestamp=2026-01-07T18:16:22.897721263Z level=info service=smart-meter-core message="Meter system shut down" device_id=smart-meter-001
```

## Scenario 2: Device Initialization

After the device was provisioned, it was able to connect to mqtt edge node and request authorization.

The device had no balance yet so the authorization was rejected and the device did not allowed to turn on appliances.

```
smartmeter  | 2026-01-07T19:20:30.670785918Z timestamp=2026-01-07T19:20:30.670566376Z level=info service=smart-meter-core message="Connecting to MQTT broker..." device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.707910710Z timestamp=2026-01-07T19:20:30.707672918Z level=info service=smart-meter-core message="Connected to MQTT broker" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.714570043Z timestamp=2026-01-07T19:20:30.714354543Z level=info service=device_interface message="Subscribed to topic on device mqtt" topic=/devices/smart-meter-001/response/authorize
smartmeter  | 2026-01-07T19:20:30.719133001Z timestamp=2026-01-07T19:20:30.718727085Z level=info service=device_interface message="Subscribed to topic on device mqtt" topic=/devices/smart-meter-001/events/invoice
smartmeter  | 2026-01-07T19:20:30.719161585Z timestamp=2026-01-07T19:20:30.718768668Z level=info service=smart-meter-core message="Subscribed to invoice events: /devices/smart-meter-001/events/invoice" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.719213460Z timestamp=2026-01-07T19:20:30.718788668Z level=info service=device_interface message="Subscribed to topic on device mqtt" topic=/devices/smart-meter-001/response/invoice
smartmeter  | 2026-01-07T19:20:30.719222543Z timestamp=2026-01-07T19:20:30.718819335Z level=info service=device_interface message="Subscribed to topic on device mqtt" topic=/devices/smart-meter-001/config
smartmeter  | 2026-01-07T19:20:30.719228418Z timestamp=2026-01-07T19:20:30.718830501Z level=info service=device_interface message="Subscribed to topic on device mqtt" topic=/devices/smart-meter-001/balance
smartmeter  | 2026-01-07T19:20:30.719231501Z timestamp=2026-01-07T19:20:30.718876293Z level=info service=device_interface message="Subscribed to topic on device mqtt" topic=/devices/smart-meter-001/control
smartmeter  | 2026-01-07T19:20:30.720815543Z timestamp=2026-01-07T19:20:30.720497251Z level=info service=smart-meter-core message="Configuration updated" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.720838876Z timestamp=2026-01-07T19:20:30.720559751Z level=info service=smart-meter-core message="Usage reporting interval updated to 1 seconds" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.773706002Z timestamp=2026-01-07T19:20:30.773347543Z level=info service=device_interface message="All subscriptions established, ready to send messages on device mqtt" subscribed=6 total=6
smartmeter  | 2026-01-07T19:20:30.773777210Z timestamp=2026-01-07T19:20:30.773455085Z level=info service=device_interface message="Subscriptions ready, proceeding with startup sequence on device mqtt" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.780482043Z timestamp=2026-01-07T19:20:30.780269877Z level=info service=smart-meter-core message="Authorization requested (20260107192030-916jbm): 1000 msat for STARTUP" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:30.780550918Z timestamp=2026-01-07T19:20:30.780325085Z level=info service=smart-meter-core message="Device connected and ready" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:31.290613752Z timestamp=2026-01-07T19:20:31.290116002Z level=error service=smart-meter-core message="Authorization rejected: 20260107192030-916jbm" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:31.292833335Z timestamp=2026-01-07T19:20:31.292654168Z level=warn service=smart-meter-core message="Command STOP received: INSUFFICIENT_FUNDS" device_id=smart-meter-001
```

## Scenario 3: Funding a Device via Lightning

The device had balance of 0 msat.

A invoice was requested to add 10.000 msat.

The invoice was paid manually via the LND Node B in Polar Lightning.

The edge node detected and have published the new balance update and RESUME command.

```
smartmeter  | 2026-01-07T19:20:36.701758504Z timestamp=2026-01-07T19:20:36.701450463Z level=info service=smart-meter-core message="Invoice request sent (20260107192036-r7b5n2): 10000 msat for USER_TOPUP" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:37.361985088Z timestamp=2026-01-07T19:20:37.361826046Z level=info service=smart-meter-core message="Invoice created: f7d42220fe8296b4aea1cfbda7891cb00d892b176dd618885aca582188175ca2" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.145212883Z timestamp=2026-01-07T19:20:44.145096425Z level=info service=smart-meter-core message="Invoice settled: f7d42220fe8296b4aea1cfbda7891cb00d892b176dd618885aca582188175ca2 (10000 msats received)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.151174716Z timestamp=2026-01-07T19:20:44.15104505Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.151189466Z timestamp=2026-01-07T19:20:44.15106605Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
```

## Scenario 4: Authorization and Prepaid Allowance

After the balance was added to the device, the smartmeter requested a new authorization to be able to perform its services.

```
smartmeter  | 2026-01-07T19:20:44.163846383Z timestamp=2026-01-07T19:20:44.163783091Z level=info service=smart-meter-core message="Authorization requested (20260107192044-4cw66w): 1000 msat for FUNDS_AVAILABLE" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.163862966Z timestamp=2026-01-07T19:20:44.163804883Z level=info service=smart-meter-core message="Balance updated: 10000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.218214800Z timestamp=2026-01-07T19:20:44.218046341Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.223055758Z timestamp=2026-01-07T19:20:44.222902758Z level=info service=smart-meter-core message="Balance updated: 9000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.223059341Z timestamp=2026-01-07T19:20:44.222946675Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:44.223060425Z timestamp=2026-01-07T19:20:44.222954508Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
```

## Scenario 5: Usage Reporting and Debit Application

As per expected, 10 authorization requests was made until exausthed the whole balance.

Each usage report consumed 100 msat.

```
smartmeter  | 2026-01-07T19:20:49.237813427Z timestamp=2026-01-07T19:20:49.237418594Z level=info service=smart-meter-core message="ToggleAppliance called" appliance_id=fridge
smartmeter  | 2026-01-07T19:20:49.237825552Z timestamp=2026-01-07T19:20:49.237462469Z level=info service=smart-meter-core message="Refrigerator turned ON" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:49.723197261Z timestamp=2026-01-07T19:20:49.721468386Z level=info service=smart-meter-core message="Usage report sent (20260107192049-kh8d2x): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:50.723157678Z timestamp=2026-01-07T19:20:50.722418428Z level=info service=smart-meter-core message="Usage report sent (20260107192050-srzzhl): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:51.722487762Z timestamp=2026-01-07T19:20:51.722356928Z level=info service=smart-meter-core message="Usage report sent (20260107192051-r2sqlj): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:52.722388345Z timestamp=2026-01-07T19:20:52.721091054Z level=info service=smart-meter-core message="Usage report sent (20260107192052-26jndw): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:53.721449637Z timestamp=2026-01-07T19:20:53.721123012Z level=info service=smart-meter-core message="Usage report sent (20260107192053-kc1z63): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:54.722389638Z timestamp=2026-01-07T19:20:54.722004555Z level=info service=smart-meter-core message="Usage report sent (20260107192054-3gyyry): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:55.721383680Z timestamp=2026-01-07T19:20:55.720917722Z level=info service=smart-meter-core message="Usage report sent (20260107192055-lt90h5): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:56.723524291Z timestamp=2026-01-07T19:20:56.723136083Z level=info service=smart-meter-core message="Usage report sent (20260107192056-9krkkr): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:57.724436167Z timestamp=2026-01-07T19:20:57.724013958Z level=info service=smart-meter-core message="Usage report sent (20260107192057-bas3ct): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.722665376Z timestamp=2026-01-07T19:20:58.722131417Z level=info service=smart-meter-core message="Usage report sent (20260107192058-tyouhn): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.756169126Z timestamp=2026-01-07T19:20:58.755750167Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.762028584Z timestamp=2026-01-07T19:20:58.761808376Z level=info service=smart-meter-core message="Authorization requested (20260107192058-atebvy): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.816230584Z timestamp=2026-01-07T19:20:58.815779042Z level=info service=smart-meter-core message="Balance updated: 8000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.822930667Z timestamp=2026-01-07T19:20:58.822664042Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.822966084Z timestamp=2026-01-07T19:20:58.822726042Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:58.822969667Z timestamp=2026-01-07T19:20:58.822733167Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:20:59.724875043Z timestamp=2026-01-07T19:20:59.724474168Z level=info service=smart-meter-core message="Usage report sent (20260107192059-nkzg2r): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:00.724432460Z timestamp=2026-01-07T19:21:00.724155918Z level=info service=smart-meter-core message="Usage report sent (20260107192100-n49kok): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:01.723561419Z timestamp=2026-01-07T19:21:01.723249335Z level=info service=smart-meter-core message="Usage report sent (20260107192101-01ujm1): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:02.723367044Z timestamp=2026-01-07T19:21:02.723049044Z level=info service=smart-meter-core message="Usage report sent (20260107192102-ukvvn4): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:03.723527920Z timestamp=2026-01-07T19:21:03.723092086Z level=info service=smart-meter-core message="Usage report sent (20260107192103-r3p42h): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:04.722627253Z timestamp=2026-01-07T19:21:04.722139837Z level=info service=smart-meter-core message="Usage report sent (20260107192104-dnjw3j): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:05.723590712Z timestamp=2026-01-07T19:21:05.723152462Z level=info service=smart-meter-core message="Usage report sent (20260107192105-qez27u): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:06.724120754Z timestamp=2026-01-07T19:21:06.723582338Z level=info service=smart-meter-core message="Usage report sent (20260107192106-9y287z): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:07.722612630Z timestamp=2026-01-07T19:21:07.72218313Z level=info service=smart-meter-core message="Usage report sent (20260107192107-agmk3p): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.724133214Z timestamp=2026-01-07T19:21:08.723844505Z level=info service=smart-meter-core message="Usage report sent (20260107192108-8blluu): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.748956797Z timestamp=2026-01-07T19:21:08.748516505Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.754976422Z timestamp=2026-01-07T19:21:08.754468089Z level=info service=smart-meter-core message="Authorization requested (20260107192108-kwbf8u): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.798370297Z timestamp=2026-01-07T19:21:08.798055255Z level=info service=smart-meter-core message="Balance updated: 7000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.803295922Z timestamp=2026-01-07T19:21:08.803041755Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.808746505Z timestamp=2026-01-07T19:21:08.808336839Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:08.808781839Z timestamp=2026-01-07T19:21:08.808381964Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:09.725167797Z timestamp=2026-01-07T19:21:09.724768589Z level=info service=smart-meter-core message="Usage report sent (20260107192109-x90qww): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:10.723621631Z timestamp=2026-01-07T19:21:10.723318506Z level=info service=smart-meter-core message="Usage report sent (20260107192110-kqc2y1): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:11.725473007Z timestamp=2026-01-07T19:21:11.725088382Z level=info service=smart-meter-core message="Usage report sent (20260107192111-vk5r3a): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:12.723620049Z timestamp=2026-01-07T19:21:12.723208091Z level=info service=smart-meter-core message="Usage report sent (20260107192112-nyrn4o): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:13.723192174Z timestamp=2026-01-07T19:21:13.723072258Z level=info service=smart-meter-core message="Usage report sent (20260107192113-u78kbb): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:14.722982800Z timestamp=2026-01-07T19:21:14.722601591Z level=info service=smart-meter-core message="Usage report sent (20260107192114-ip3dxj): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:15.724013884Z timestamp=2026-01-07T19:21:15.723579717Z level=info service=smart-meter-core message="Usage report sent (20260107192115-jh23ch): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:16.723867551Z timestamp=2026-01-07T19:21:16.723490342Z level=info service=smart-meter-core message="Usage report sent (20260107192116-36e0lm): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:17.722093676Z timestamp=2026-01-07T19:21:17.721958468Z level=info service=smart-meter-core message="Usage report sent (20260107192117-k45pgp): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.722553052Z timestamp=2026-01-07T19:21:18.722101135Z level=info service=smart-meter-core message="Usage report sent (20260107192118-eg116s): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.754625510Z timestamp=2026-01-07T19:21:18.75418976Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.763198635Z timestamp=2026-01-07T19:21:18.762877885Z level=info service=smart-meter-core message="Authorization requested (20260107192118-rht7fi): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.813121552Z timestamp=2026-01-07T19:21:18.812824052Z level=info service=smart-meter-core message="Balance updated: 6000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.818781885Z timestamp=2026-01-07T19:21:18.818259593Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.818839427Z timestamp=2026-01-07T19:21:18.81829801Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:18.818846677Z timestamp=2026-01-07T19:21:18.818265968Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:19.723453886Z timestamp=2026-01-07T19:21:19.723065344Z level=info service=smart-meter-core message="Usage report sent (20260107192119-rurm9d): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:20.723060303Z timestamp=2026-01-07T19:21:20.722587594Z level=info service=smart-meter-core message="Usage report sent (20260107192120-c1kfcb): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:21.725481178Z timestamp=2026-01-07T19:21:21.724993262Z level=info service=smart-meter-core message="Usage report sent (20260107192121-mn582u): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:22.725265262Z timestamp=2026-01-07T19:21:22.724919762Z level=info service=smart-meter-core message="Usage report sent (20260107192122-ian8en): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:23.723919429Z timestamp=2026-01-07T19:21:23.723502554Z level=info service=smart-meter-core message="Usage report sent (20260107192123-7yayec): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:24.724881638Z timestamp=2026-01-07T19:21:24.724391513Z level=info service=smart-meter-core message="Usage report sent (20260107192124-himdkj): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:25.724392930Z timestamp=2026-01-07T19:21:25.723932722Z level=info service=smart-meter-core message="Usage report sent (20260107192125-cda0c7): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:26.723713666Z timestamp=2026-01-07T19:21:26.723437458Z level=info service=smart-meter-core message="Usage report sent (20260107192126-k950qn): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:27.725645750Z timestamp=2026-01-07T19:21:27.725054708Z level=info service=smart-meter-core message="Usage report sent (20260107192127-lbpg23): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.723145042Z timestamp=2026-01-07T19:21:28.722788834Z level=info service=smart-meter-core message="Usage report sent (20260107192128-khvkkm): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.762713084Z timestamp=2026-01-07T19:21:28.762103376Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.771703751Z timestamp=2026-01-07T19:21:28.771515084Z level=info service=smart-meter-core message="Authorization requested (20260107192128-8b9ty8): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.819357417Z timestamp=2026-01-07T19:21:28.818822792Z level=info service=smart-meter-core message="Balance updated: 5000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.823743459Z timestamp=2026-01-07T19:21:28.823549084Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.823797667Z timestamp=2026-01-07T19:21:28.823589334Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:28.823813417Z timestamp=2026-01-07T19:21:28.823702292Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:29.726237251Z timestamp=2026-01-07T19:21:29.725775376Z level=info service=smart-meter-core message="Usage report sent (20260107192129-700yk7): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:30.725839460Z timestamp=2026-01-07T19:21:30.725431793Z level=info service=smart-meter-core message="Usage report sent (20260107192130-3uz9gp): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:31.724869460Z timestamp=2026-01-07T19:21:31.724224585Z level=info service=smart-meter-core message="Usage report sent (20260107192131-b00evo): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:32.725661544Z timestamp=2026-01-07T19:21:32.725086252Z level=info service=smart-meter-core message="Usage report sent (20260107192132-wwgacw): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:33.723082336Z timestamp=2026-01-07T19:21:33.722634711Z level=info service=smart-meter-core message="Usage report sent (20260107192133-wjpa2o): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:34.723518878Z timestamp=2026-01-07T19:21:34.722940712Z level=info service=smart-meter-core message="Usage report sent (20260107192134-xorq8o): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:35.724334671Z timestamp=2026-01-07T19:21:35.723928004Z level=info service=smart-meter-core message="Usage report sent (20260107192135-7mzn9z): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:36.725080713Z timestamp=2026-01-07T19:21:36.724647963Z level=info service=smart-meter-core message="Usage report sent (20260107192136-taojnk): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:37.725598171Z timestamp=2026-01-07T19:21:37.725089671Z level=info service=smart-meter-core message="Usage report sent (20260107192137-g0oixs): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.726064005Z timestamp=2026-01-07T19:21:38.725602464Z level=info service=smart-meter-core message="Usage report sent (20260107192138-h8o4tv): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.752403255Z timestamp=2026-01-07T19:21:38.751092089Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.756920880Z timestamp=2026-01-07T19:21:38.756671922Z level=info service=smart-meter-core message="Authorization requested (20260107192138-9gmr4d): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.804862172Z timestamp=2026-01-07T19:21:38.804447589Z level=info service=smart-meter-core message="Balance updated: 4000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.811225630Z timestamp=2026-01-07T19:21:38.810913589Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.811271505Z timestamp=2026-01-07T19:21:38.81094863Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:38.811412547Z timestamp=2026-01-07T19:21:38.810921255Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:39.726030547Z timestamp=2026-01-07T19:21:39.725635756Z level=info service=smart-meter-core message="Usage report sent (20260107192139-jcmglg): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:40.725732506Z timestamp=2026-01-07T19:21:40.725277631Z level=info service=smart-meter-core message="Usage report sent (20260107192140-na4l61): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:41.723998173Z timestamp=2026-01-07T19:21:41.72370884Z level=info service=smart-meter-core message="Usage report sent (20260107192141-yrepi6): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:42.725891049Z timestamp=2026-01-07T19:21:42.725506799Z level=info service=smart-meter-core message="Usage report sent (20260107192142-7esydj): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:43.723317299Z timestamp=2026-01-07T19:21:43.723200133Z level=info service=smart-meter-core message="Usage report sent (20260107192143-zqt80x): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:44.724168550Z timestamp=2026-01-07T19:21:44.723638425Z level=info service=smart-meter-core message="Usage report sent (20260107192144-yjtyof): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:45.723588342Z timestamp=2026-01-07T19:21:45.723060384Z level=info service=smart-meter-core message="Usage report sent (20260107192145-a0rb9u): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:46.723415134Z timestamp=2026-01-07T19:21:46.722961217Z level=info service=smart-meter-core message="Usage report sent (20260107192146-psrbd7): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:47.723507801Z timestamp=2026-01-07T19:21:47.722717093Z level=info service=smart-meter-core message="Usage report sent (20260107192147-pyq7tv): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.725893135Z timestamp=2026-01-07T19:21:48.725211968Z level=info service=smart-meter-core message="Usage report sent (20260107192148-zopd4z): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.755545510Z timestamp=2026-01-07T19:21:48.755099802Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.762667885Z timestamp=2026-01-07T19:21:48.76234176Z level=info service=smart-meter-core message="Authorization requested (20260107192148-16n17z): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.808850968Z timestamp=2026-01-07T19:21:48.808492552Z level=info service=smart-meter-core message="Balance updated: 3000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.811639427Z timestamp=2026-01-07T19:21:48.811404968Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.811690718Z timestamp=2026-01-07T19:21:48.811442468Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:48.811695135Z timestamp=2026-01-07T19:21:48.811458552Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:49.723280761Z timestamp=2026-01-07T19:21:49.722948302Z level=info service=smart-meter-core message="Usage report sent (20260107192149-ar4tuu): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:50.724114844Z timestamp=2026-01-07T19:21:50.723822553Z level=info service=smart-meter-core message="Usage report sent (20260107192150-z754p2): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:51.723875303Z timestamp=2026-01-07T19:21:51.723431303Z level=info service=smart-meter-core message="Usage report sent (20260107192151-nzw3ih): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:52.725992595Z timestamp=2026-01-07T19:21:52.725642345Z level=info service=smart-meter-core message="Usage report sent (20260107192152-doluic): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:53.723684596Z timestamp=2026-01-07T19:21:53.723275804Z level=info service=smart-meter-core message="Usage report sent (20260107192153-ku436x): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:54.723627388Z timestamp=2026-01-07T19:21:54.722986888Z level=info service=smart-meter-core message="Usage report sent (20260107192154-j52jl6): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:55.723386097Z timestamp=2026-01-07T19:21:55.722885805Z level=info service=smart-meter-core message="Usage report sent (20260107192155-ftescp): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:56.725017916Z timestamp=2026-01-07T19:21:56.724430416Z level=info service=smart-meter-core message="Usage report sent (20260107192156-k6y7pb): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:57.723413667Z timestamp=2026-01-07T19:21:57.723044458Z level=info service=smart-meter-core message="Usage report sent (20260107192157-ziy636): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.726452542Z timestamp=2026-01-07T19:21:58.726092959Z level=info service=smart-meter-core message="Usage report sent (20260107192158-b9fdnu): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.772744834Z timestamp=2026-01-07T19:21:58.772433542Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.778992834Z timestamp=2026-01-07T19:21:58.778823876Z level=info service=smart-meter-core message="Authorization requested (20260107192158-5unv4m): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.815527542Z timestamp=2026-01-07T19:21:58.815281626Z level=info service=smart-meter-core message="Balance updated: 2000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.835632126Z timestamp=2026-01-07T19:21:58.823190959Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.835680751Z timestamp=2026-01-07T19:21:58.823223209Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:58.835683126Z timestamp=2026-01-07T19:21:58.823206209Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:21:59.723345293Z timestamp=2026-01-07T19:21:59.722932001Z level=info service=smart-meter-core message="Usage report sent (20260107192159-scot8w): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:00.724278001Z timestamp=2026-01-07T19:22:00.723942751Z level=info service=smart-meter-core message="Usage report sent (20260107192200-kmuhps): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:01.723941085Z timestamp=2026-01-07T19:22:01.72356621Z level=info service=smart-meter-core message="Usage report sent (20260107192201-h1fex8): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:02.723617002Z timestamp=2026-01-07T19:22:02.723358544Z level=info service=smart-meter-core message="Usage report sent (20260107192202-akl0b8): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:03.724910128Z timestamp=2026-01-07T19:22:03.724428503Z level=info service=smart-meter-core message="Usage report sent (20260107192203-4ots8w): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:04.724955503Z timestamp=2026-01-07T19:22:04.724378545Z level=info service=smart-meter-core message="Usage report sent (20260107192204-ub6l34): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:05.723369962Z timestamp=2026-01-07T19:22:05.723170671Z level=info service=smart-meter-core message="Usage report sent (20260107192205-2itx3n): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:06.723478838Z timestamp=2026-01-07T19:22:06.723082838Z level=info service=smart-meter-core message="Usage report sent (20260107192206-4n25il): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:07.726689921Z timestamp=2026-01-07T19:22:07.725866296Z level=info service=smart-meter-core message="Usage report sent (20260107192207-k5fc5w): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.725870255Z timestamp=2026-01-07T19:22:08.72552938Z level=info service=smart-meter-core message="Usage report sent (20260107192208-xkd31k): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.754199714Z timestamp=2026-01-07T19:22:08.753609672Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.760350755Z timestamp=2026-01-07T19:22:08.759940547Z level=info service=smart-meter-core message="Authorization requested (20260107192208-0gh9we): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.803751755Z timestamp=2026-01-07T19:22:08.802931714Z level=info service=smart-meter-core message="Balance updated: 1000 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.812547214Z timestamp=2026-01-07T19:22:08.812198589Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.812597797Z timestamp=2026-01-07T19:22:08.812244005Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:08.812652589Z timestamp=2026-01-07T19:22:08.812335047Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:09.724319631Z timestamp=2026-01-07T19:22:09.723823839Z level=info service=smart-meter-core message="Usage report sent (20260107192209-z0fzwx): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:10.724711006Z timestamp=2026-01-07T19:22:10.724426256Z level=info service=smart-meter-core message="Usage report sent (20260107192210-45y6jp): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:11.723520757Z timestamp=2026-01-07T19:22:11.723081507Z level=info service=smart-meter-core message="Usage report sent (20260107192211-t8eptd): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:12.725441507Z timestamp=2026-01-07T19:22:12.725143382Z level=info service=smart-meter-core message="Usage report sent (20260107192212-ra1wka): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:13.722922258Z timestamp=2026-01-07T19:22:13.722843591Z level=info service=smart-meter-core message="Usage report sent (20260107192213-9pm5sx): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:14.724929633Z timestamp=2026-01-07T19:22:14.724580383Z level=info service=smart-meter-core message="Usage report sent (20260107192214-trdx1y): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:15.724840925Z timestamp=2026-01-07T19:22:15.724406134Z level=info service=smart-meter-core message="Usage report sent (20260107192215-8gl9t1): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:16.723845217Z timestamp=2026-01-07T19:22:16.723580259Z level=info service=smart-meter-core message="Usage report sent (20260107192216-c6yeo5): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:17.724764885Z timestamp=2026-01-07T19:22:17.724182551Z level=info service=smart-meter-core message="Usage report sent (20260107192217-bj2hmx): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.726132718Z timestamp=2026-01-07T19:22:18.725459302Z level=info service=smart-meter-core message="Usage report sent (20260107192218-peb6ru): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.746910927Z timestamp=2026-01-07T19:22:18.746285635Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.752559218Z timestamp=2026-01-07T19:22:18.752383968Z level=info service=smart-meter-core message="Authorization requested (20260107192218-5jif29): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.798128718Z timestamp=2026-01-07T19:22:18.797729968Z level=info service=smart-meter-core message="Balance updated: 0 msat available" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.811199468Z timestamp=2026-01-07T19:22:18.810761052Z level=info service=smart-meter-core message="Command RESUME received" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.811256677Z timestamp=2026-01-07T19:22:18.810853385Z level=info service=smart-meter-core message="Appliances resumed" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:18.811263177Z timestamp=2026-01-07T19:22:18.810781593Z level=info service=smart-meter-core message="Authorization granted: 1000 msat (reserved)" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:19.724787886Z timestamp=2026-01-07T19:22:19.724418802Z level=info service=smart-meter-core message="Usage report sent (20260107192219-i14ory): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:20.725149178Z timestamp=2026-01-07T19:22:20.724824178Z level=info service=smart-meter-core message="Usage report sent (20260107192220-3ae6m1): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:21.726377928Z timestamp=2026-01-07T19:22:21.726015803Z level=info service=smart-meter-core message="Usage report sent (20260107192221-ay2vyt): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:22.723768762Z timestamp=2026-01-07T19:22:22.72328647Z level=info service=smart-meter-core message="Usage report sent (20260107192222-l2gew0): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:23.725977221Z timestamp=2026-01-07T19:22:23.725408971Z level=info service=smart-meter-core message="Usage report sent (20260107192223-7hnok9): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:24.726327680Z timestamp=2026-01-07T19:22:24.725566888Z level=info service=smart-meter-core message="Usage report sent (20260107192224-dqeumr): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:25.724776305Z timestamp=2026-01-07T19:22:25.724366888Z level=info service=smart-meter-core message="Usage report sent (20260107192225-412q9f): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:26.726342708Z timestamp=2026-01-07T19:22:26.725703541Z level=info service=smart-meter-core message="Usage report sent (20260107192226-hshcyw): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:27.726655458Z timestamp=2026-01-07T19:22:27.726087292Z level=info service=smart-meter-core message="Usage report sent (20260107192227-1c8dqi): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:28.725589167Z timestamp=2026-01-07T19:22:28.725096167Z level=info service=smart-meter-core message="Usage report sent (20260107192228-z4cns7): 0.0010 kWh" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:28.767760334Z timestamp=2026-01-07T19:22:28.767213167Z level=info service=smart-meter-core message="Command AUTHORIZATION received (reason: COMPLETED)" device_id=smart-meter-001
```

## Scenario 6: Allowance Exhaustion and Device Interruption

Once the system has reached balance of 0, the next authoriation request was rejected which caused the edge node to send STOP command with INSUFFICIENT_FUNDS to the device as expected.

```
smartmeter  | 2026-01-07T19:22:28.776579334Z timestamp=2026-01-07T19:22:28.776338084Z level=info service=smart-meter-core message="Authorization requested (20260107192228-ftuzj9): 1000 msat for COMPLETED" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:28.819995042Z timestamp=2026-01-07T19:22:28.819536834Z level=error service=smart-meter-core message="Authorization rejected: 20260107192228-ftuzj9" device_id=smart-meter-001
smartmeter  | 2026-01-07T19:22:28.825416501Z timestamp=2026-01-07T19:22:28.825109126Z level=warn service=smart-meter-core message="Command STOP received: INSUFFICIENT_FUNDS" device_id=smart-meter-001
```

