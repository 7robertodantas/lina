# Mosquitto MQTT Broker Configuration

This directory contains the configuration for the Eclipse Mosquitto MQTT broker.

## Directory Structure

- `config/` - Mosquitto configuration files
- `data/` - Persistence data (gitignored)
- `log/` - Log files (gitignored)
- `certs/` - TLS certificates (gitignored)

## TLS Certificates

Place your TLS certificates in the `certs/` directory:

### Required for TLS (Server-side):
- **`ca.crt`** - Certificate Authority certificate
  - Used by: Both server and clients
  - Purpose: Root of trust to verify certificates
  
- **`server.crt`** - Server certificate
  - Used by: Mosquitto broker
  - Purpose: Identifies the broker to clients (configured in `mosquitto.conf` lines 11-13)
  
- **`server.key`** - Server private key
  - Used by: Mosquitto broker
  - Purpose: Private key for server certificate

### Optional (Edge Node authentication):
- **`edge.crt`** - Edge node certificate
  - Used by: Edge node services (like device service)
  - Purpose: Authenticates the edge node to the broker
  - Only needed if `require_certificate true` in mosquitto.conf
  - **Note**: This is ONE certificate for the edge node, not per physical device
  
- **`edge.key`** - Edge node private key
  - Used by: Edge node services
  - Purpose: Private key for edge node certificate

**Certificate Strategy:**

**For Edge Node (device service):**
- Use **one shared certificate** (`edge.crt` + `edge.key`) for the edge node service
- This represents the edge node itself, not individual physical devices
- If the edge node is compromised, you can revoke this single certificate

**For Physical IoT Devices:**
- **Option 1**: Username/password authentication (simpler, recommended for most cases)
- **Option 2**: Per-device certificates (more secure, better for revocation, but complex to manage)
  - Each physical device would have its own certificate
  - Requires certificate provisioning and management infrastructure
  - Better for high-security scenarios

**Current Configuration:**
- Mosquitto uses: `ca.crt`, `server.crt`, `server.key` (configured in `mosquitto.conf`)
- Edge node (device service) uses: `ca.crt` (to verify server), optionally `edge.crt` + `edge.key` (if cert auth enabled)
- Physical devices: Use username/password (or their own certs if per-device certs are implemented)
- `require_certificate false` means edge node certificates are optional

## Generating Test Certificates

### Recommended: Use the generation script

The easiest way to generate certificates is using the provided script:

```bash
cd mosquitto
./generate-certs.sh
```

This script will:
- Check if certificates already exist (won't overwrite existing ones)
- Generate CA, server, and edge node certificates if missing
- Clean up temporary files automatically

### Manual generation

If you prefer to generate certificates manually:

```bash
# Generate CA key and certificate
openssl genrsa -out certs/ca.key 2048
openssl req -new -x509 -days 365 -key certs/ca.key -out certs/ca.crt -subj "/CN=MQTT-CA"

# Generate server key and certificate
openssl genrsa -out certs/server.key 2048
openssl req -new -key certs/server.key -out certs/server.csr -subj "/CN=mosquitto"
openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/server.crt -days 365

# Generate edge node key and certificate (optional)
openssl genrsa -out certs/edge.key 2048
openssl req -new -key certs/edge.key -out certs/edge.csr -subj "/CN=edge-node"
openssl x509 -req -in certs/edge.csr -CA certs/ca.crt -CAkey certs/ca.key -CAcreateserial -out certs/edge.crt -days 365

# Clean up CSR files
rm certs/*.csr certs/*.srl
```

## Configuration

The Mosquitto configuration is managed via `docker-compose.test.yml` environment variables:

- `MQTT_PORT` - Non-TLS port (default: 1883)
- `MQTT_TLS_PORT` - TLS port (default: 8883)
- `MQTT_TLS_CERT_FILE` - Server certificate path
- `MQTT_TLS_KEY_FILE` - Server key path
- `MQTT_TLS_CA_FILE` - CA certificate path
- `MQTT_REQUIRE_CERTIFICATE` - Require client certificates (default: false)
- `MQTT_ALLOW_ANONYMOUS` - Allow anonymous connections (default: true)

