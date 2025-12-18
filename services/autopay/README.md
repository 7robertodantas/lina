# Autopay Service

A sidecar service that automatically pays Lightning invoices for devices during load testing.

## Overview

The autopay service listens directly to LND's invoice stream and automatically pays invoices when they are created. This is designed for load testing scenarios where devices need funds but manual payment is not feasible.

## How It Works

1. **Device requests invoice**: Devices publish invoice requests via MQTT
2. **System creates invoice**: The lightning service creates an invoice in the receiver LND node
3. **Invoice stream notification**: The receiver LND node emits an invoice creation event via its stream
4. **Autopay service intercepts**: The autopay service subscribes to the receiver LND node's invoice stream
5. **Auto-payment**: When an invoice is created (OPEN/ACCEPTED state), the service automatically pays it using the payer LND node's `SendPayment` API

## Configuration

The service requires the following environment variables:

### Receiver LND Configuration (the node that creates invoices)
- `RECEIVER_LND_HOST`: Receiver LND gRPC host (e.g., `localhost:10009`)
  - Falls back to `LND_HOST` if not specified
- `RECEIVER_LND_TLS_CERT_HEX`: Hex-encoded TLS certificate for receiver LND
  - Falls back to `LND_TLS_CERT_HEX` if not specified
- `RECEIVER_LND_TLS_SERVER_NAME`: TLS server name (default: `localhost`)
  - Falls back to `LND_TLS_SERVER_NAME` if not specified
- `RECEIVER_LND_MACAROON_HEX`: Hex-encoded macaroon for receiver LND authentication
  - Falls back to `LND_MACAROON_HEX` if not specified

### Payer LND Configuration (the node that pays invoices)
- `PAYER_LND_HOST`: Payer LND gRPC host (e.g., `localhost:10009`)
  - Falls back to `LND_HOST` if not specified (can be same as receiver)
- `PAYER_LND_TLS_CERT_HEX`: Hex-encoded TLS certificate for payer LND
  - Falls back to `LND_TLS_CERT_HEX` if not specified
- `PAYER_LND_TLS_SERVER_NAME`: TLS server name (default: `localhost`)
  - Falls back to `LND_TLS_SERVER_NAME` if not specified
- `PAYER_LND_MACAROON_HEX`: Hex-encoded macaroon for payer LND authentication
  - Falls back to `LND_MACAROON_HEX` if not specified

### General Configuration
- `NETWORK`: Bitcoin network (default: `regtest`)

**Note**: If receiver and payer are the same node, you only need to set the base `LND_*` variables and both will use the same node.

## Building

```bash
docker build -f autopay/Dockerfile -t 7robertodantas/lnpay-autopay:latest .
```

## Running

### Using Docker Compose

The service is included in `docker-compose.loadtest.edge.yml` and will automatically start with the measurement stack.

You can also run it standalone using `docker-compose.autopay.yml`:

```bash
# Make sure your .env file has the LND configuration
docker-compose -f docker-compose.autopay.yml up -d
```

### Standalone Docker

```bash
docker run -d \
  --name autopay \
  --env-file .env \
  -e RECEIVER_LND_HOST=${RECEIVER_LND_HOST:-${LND_HOST}} \
  -e RECEIVER_LND_TLS_CERT_HEX=${RECEIVER_LND_TLS_CERT_HEX:-${LND_TLS_CERT_HEX}} \
  -e RECEIVER_LND_MACAROON_HEX=${RECEIVER_LND_MACAROON_HEX:-${LND_MACAROON_HEX}} \
  -e PAYER_LND_HOST=${PAYER_LND_HOST:-${LND_HOST}} \
  -e PAYER_LND_TLS_CERT_HEX=${PAYER_LND_TLS_CERT_HEX:-${LND_TLS_CERT_HEX}} \
  -e PAYER_LND_MACAROON_HEX=${PAYER_LND_MACAROON_HEX:-${LND_MACAROON_HEX}} \
  7robertodantas/lnpay-autopay:latest
```

## Limitations

- **Load testing only**: This service is designed for load testing scenarios. In production, invoices should be paid by actual users.
- **No payment verification**: The service assumes all invoices should be paid. It doesn't verify device authorization or payment limits.
- **Single LND node**: The service connects to a single LND node. For multi-node setups, you may need multiple instances.

## Architecture Decision

We chose a direct LND stream approach over:
- **MQTT-based approach**: Adds unnecessary dependency and complexity
- **Mocking LND**: Wouldn't test real Lightning Network integration
- **Modifying load test script**: Would be complex and not reusable
- **Manual payment**: Not feasible for automated load testing

The direct LND stream approach:
- ✅ Tests real LND integration
- ✅ No MQTT dependency - simpler architecture
- ✅ Direct integration with LND's native streaming API
- ✅ More reliable - listening to source of truth (LND)
- ✅ Keeps load test script simple
- ✅ Reusable across different test scenarios
- ✅ Can be easily disabled for production

