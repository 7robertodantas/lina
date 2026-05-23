# k6 Load Testing for LINA

This directory contains k6 load testing scripts to simulate thousands of devices connecting to the LINA system.

## Prerequisites

1. **Install k6**: https://k6.io/docs/getting-started/installation/
   ```bash
   # macOS
   brew install k6
   
   # Linux (Debian/Ubuntu)
   sudo gpg -k
   sudo gpg --no-default-keyring --keyring /usr/share/keyrings/k6-archive-keyring.gpg --keyserver hkp://keyserver.ubuntu.com:80 --recv-keys C5AD17C747E3415A3642D57D77C6C491D6AC1D9
   echo "deb [signed-by=/usr/share/keyrings/k6-archive-keyring.gpg] https://dl.k6.io/deb stable main" | sudo tee /etc/apt/sources.list.d/k6.list
   sudo apt-get update
   sudo apt-get install k6
   
   # Or download from: https://github.com/grafana/k6/releases
   ```

2. **Install k6 MQTT extension**:
   
   k6 extensions require building a custom k6 binary using `xk6`:
   ```bash
   # Install xk6 (Go tool)
   go install go.k6.io/xk6/cmd/xk6@latest
   
   # Build k6 with MQTT extension
   xk6 build --with github.com/wgarcia4190/xk6-mqtt@latest
   
   # This creates a custom k6 binary in the current directory
   # Use this binary instead of the system k6: ./k6 run loadtest.js
   ```
   
   **Note**: You'll need to use the custom-built `k6` binary (not the system one) when running tests that use the MQTT extension.

3. **Ensure you have access to**:
   - The device service API (default: http://localhost:8080)
   - The MQTT broker (default: localhost:8883)

## Configuration

The script can be configured via environment variables:

- `LOG_LEVEL`: Script log verbosity — `debug`, `info`, `warn`, or `error` (default: `info`). Matches backend `LOG_LEVEL`: at `info`, per-request success lines and batch progress are hidden; use `debug` for those. Ramp **stage target** lines (`[loadtest] ramp stage …`) are always printed to stdout (VU 1) so you can see VU targets regardless of `LOG_LEVEL`. The active level is printed once at setup: `[loadtest] LOG_LEVEL=…`.
- `LEVEL_VUS`: Number of simulated devices added per load level (default: `25`)
- `LEVEL_COUNT`: Number of load levels to execute (default: `8`)
- `IDLE`: Initial idle stage duration (default: `30s`)
- `WARMUP`: Warmup/ramp duration per load level (default: `60s`)
- `MEASURE`: Measurement duration per load level (default: `120s`)
- `TEARDOWN`: Ramp-down duration after the final level (default: `60s`)
- `API_BASE_URL`: Base URL for the device service API (default: `http://localhost:8080`)
- `API_DEVICES_BATCH_ENDPOINT`: API endpoint path for batch device registration (default: `/devices/batch`)
- `HTTPDEVICE_BASE_URL`: Base URL for the HTTP device simulator (default: `http://localhost:3002`)
- `USAGE_REPORT_INTERVAL`: Interval in seconds between usage reports per simulated device (default: `1`)
- `UNIT_PRICE_MSAT`: Device unit price in millisatoshis (default: `100`)
- `AUTHORIZE_REQUEST_MSAT`: Amount in millisatoshis to request for authorization (default: `10000`)
- `CONNECT_BATCH_SIZE`: Number of devices connected per setup batch (default: `LEVEL_VUS`)
- `CONNECT_BATCH_SLEEP`: Seconds to sleep between setup connection batches (default: `5`)
- `CONNECT_TIMEOUT`: Timeout for each simulator connect request (default: `120s`)
- `TEARDOWN_DRAIN_SECONDS`: Seconds to wait before disconnecting devices in teardown (default: `45`)

When using k6 Prometheus remote write, the script also publishes experiment
marker metrics (`k6_loadtest_stage_index`, `k6_loadtest_phase_code`,
`k6_loadtest_level_vus`, and `k6_loadtest_measurement_active`). The measurement
plotting script uses these to annotate warmup, measurement, and teardown in
report graphs.

## Project Structure

```
k6-loadtest/
├── loadtest.js      # Main k6 test script
├── README.md        # This file
├── .gitignore       # Git ignore file
└── Makefile         # Convenience commands
```

**Note**: k6 doesn't use npm, package.json, or node_modules. It's a standalone tool that runs JavaScript directly.

## Running the Load Test

### Basic Usage

**Important**: For self-signed certificates, use k6's `--insecure-skip-tls-verify` flag:

```bash
k6 run --insecure-skip-tls-verify loadtest.js
```

Or if you built k6 with the MQTT extension:
```bash
./k6 run --insecure-skip-tls-verify loadtest.js
```

**Note**: The `--insecure-skip-tls-verify` flag applies globally to all TLS connections in k6, including MQTT. This is the recommended approach for testing with self-signed certificates.

### With Custom Configuration

```bash
# Accessing through Caddy (port 80)
API_BASE_URL=http://localhost:80 \
API_DEVICES_ENDPOINT=/devices \
MQTT_BROKER=mosquitto \
MQTT_TLS_PORT=8883 \
USAGE_REPORT_INTERVAL=1 \
AUTHORIZE_REQUEST_MSAT=1000000000 \
k6 run --insecure-skip-tls-verify loadtest.js

# Or accessing device service directly (port 8080)
API_BASE_URL=http://localhost:8080 \
API_DEVICES_ENDPOINT=/api/v1/devices \
MQTT_BROKER=mosquitto \
MQTT_TLS_PORT=8883 \
USAGE_REPORT_INTERVAL=1 \
AUTHORIZE_REQUEST_MSAT=1000000000 \
k6 run --insecure-skip-tls-verify loadtest.js
```

### Using Docker

If running k6 in Docker:

```bash
docker run --rm -i \
  -v $(pwd)/k6-loadtest:/scripts:ro \
  grafana/k6 run \
  --insecure-skip-tls-verify \
  --env API_BASE_URL=http://host.docker.internal:8080 \
  --env MQTT_BROKER=host.docker.internal \
  /scripts/loadtest.js
```

## Test Scenarios

The default test scenario ramps up gradually:
1. 30s: Ramp to 100 devices
2. 2m: Hold at 100 devices
3. 30s: Ramp to 500 devices
4. 2m: Hold at 500 devices
5. 30s: Ramp to 1000 devices
6. 5m: Hold at 1000 devices (stress test)
7. 30s: Ramp down to 0

You can modify the `options.stages` in `loadtest.js` to customize the load pattern.

## What the Test Does

For each virtual user (device):

1. **Setup Phase:**
   - Registers the device via `POST /api/v1/devices`
   - Connects to MQTT broker using TLS with CA certificate
   - Uses device_id as username and device_secret as password

2. **Main Test Loop:**
   - Publishes heartbeat messages to `/devices/{device_id}/heartbeat`
   - Periodically requests authorization for a large amount (configurable)
   - Publishes usage reports to `/devices/{device_id}/usage` at configurable intervals
   - Requests invoices to add funds to devices (auto-paid by autopay service)

3. **Teardown:**
   - Disconnects from MQTT broker

## Message Formats

All MQTT messages use JSON encoding (protojson format with `UseProtoNames: true`):

- **Heartbeat:**
  ```json
  {
    "device_id": "smart-meter-000001",
    "status": 1,
    "timestamp": "2025-01-15T10:30:00Z"
  }
  ```
  Note: `status` uses numeric enum value (1 = DEVICE_STATUS_ONLINE). If your server expects string enum names, you may need to adjust the script.

- **Authorization Request:**
  ```json
  {
    "device_id": "smart-meter-000001",
    "request_id": "abc123...",
    "request_msat": 1000000000,
    "reason": "STRESS_TEST",
    "timestamp": "2025-01-15T10:30:00Z"
  }
  ```

- **Usage Report:**
  ```json
  {
    "device_id": "smart-meter-000001",
    "report_id": "xyz789...",
    "strategy": 1,
    "measure": 0.05,
    "unit": "kWh",
    "timestamp": "2025-01-15T10:30:00Z"
  }
  ```
  Note: `strategy` uses numeric enum value (1 = REPORTING_STRATEGY_INTERVAL). If your server expects string enum names, you may need to adjust the script.

- **Invoice Request:**
  ```json
  {
    "device_id": "smart-meter-000001",
    "request_id": "abc123...",
    "amount_msat": 100000000,
    "reason": "USER_TOPUP",
    "timestamp": "2025-01-15T10:30:00Z"
  }
  ```
  Note: Invoice requests are automatically paid by the `autopay` sidecar service, which listens to invoice responses on MQTT and pays them via LND.

## Troubleshooting

### MQTT Connection Issues

- Verify the CA certificate path is correct
- Check that the MQTT broker is accessible
- Ensure the device is registered before connecting (setup phase)

### High Error Rates

- Reduce the number of virtual users
- Increase intervals between messages
- Check system resources (CPU, memory, network)

### Certificate Errors

- For self-signed certificates, use k6's `--insecure-skip-tls-verify` flag
- For production, use properly signed certificates or configure TLS properly
