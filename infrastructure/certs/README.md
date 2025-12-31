# Certificates Directory

This directory contains TLS certificates used across LINA services (MQTT, gRPC, etc.).

## Required Certificates

- `ca.crt` - Certificate Authority certificate (public)
- `ca.key` - Certificate Authority private key (keep secure!)
- `server.crt` - Server certificate (public)
- `server.key` - Server private key (keep secure!)

## Generating Certificates

Run the generation script:

```bash
./certs/generate-certs.sh
```

This will generate:
- CA certificate valid for 10 years (default)
- Server certificate valid for 1 year (default)

You can customize validity periods:

```bash
CA_VALIDITY_DAYS=7300 SERVER_VALIDITY_DAYS=730 ./certs/generate-certs.sh
```

## Certificate Usage

### Mosquitto Service
- Requires: `ca.crt`, `server.crt`, `server.key`
- Mounted as: `./certs:/mosquitto/certs:ro`

### Client Services (device, smartmeter)
- Requires: `ca.crt` only (public certificate for verification)
- Mounted as: `./certs/ca.crt:/certs/ca.crt:ro`
- **Note:** Private keys are NOT mounted to client services

## Security

- **Never commit private keys** (`*.key` files) to version control
- Keep `ca.key` and `server.key` secure
- Only `ca.crt` should be shared with client services
- Use proper file permissions:
  ```bash
  chmod 644 *.crt
  chmod 600 *.key
  ```

## Using Your Organization's CA

If you have an existing CA, place your certificates in this directory:
- `ca.crt` - Your organization's CA certificate
- `server.crt` - Server certificate signed by your CA
- `server.key` - Server private key

The entrypoint will use these certificates if they exist.

