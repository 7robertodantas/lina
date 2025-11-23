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
- Include Subject Alternative Names (SANs) in certificates for proper hostname verification
- Clean up temporary files automatically

**Note:** If you have existing certificates without SANs, you'll need to delete them first to regenerate with SANs:
```bash
cd mosquitto
rm -f certs/server.crt certs/server.key certs/edge.crt certs/edge.key
./generate-certs.sh
```

### Manual generation

If you prefer to generate certificates manually, **note that certificates must include Subject Alternative Names (SANs)** for proper hostname verification with Go 1.25+:

```bash
# Generate CA key and certificate
openssl genrsa -out certs/ca.key 2048
openssl req -new -x509 -days 365 -key certs/ca.key -out certs/ca.crt -subj "/CN=MQTT-CA/O=LNPay/C=US"

# Generate server key and certificate with SANs
openssl genrsa -out certs/server.key 2048

# Create server config with SANs
cat > certs/server.conf <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = mosquitto
O = LNPay
C = US

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = serverAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = mosquitto
DNS.2 = localhost
DNS.3 = *.mosquitto
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

openssl req -new -key certs/server.key -out certs/server.csr -config certs/server.conf
openssl x509 -req -in certs/server.csr -CA certs/ca.crt -CAkey certs/ca.key \
    -CAcreateserial -out certs/server.crt -days 365 \
    -extensions v3_req -extfile certs/server.conf

# Generate edge node key and certificate with SANs (optional)
openssl genrsa -out certs/edge.key 2048

# Create edge config with SANs
cat > certs/edge.conf <<EOF
[req]
distinguished_name = req_distinguished_name
req_extensions = v3_req
prompt = no

[req_distinguished_name]
CN = edge-node
O = LNPay
C = US

[v3_req]
keyUsage = keyEncipherment, dataEncipherment
extendedKeyUsage = clientAuth
subjectAltName = @alt_names

[alt_names]
DNS.1 = edge-node
DNS.2 = localhost
IP.1 = 127.0.0.1
IP.2 = ::1
EOF

openssl req -new -key certs/edge.key -out certs/edge.csr -config certs/edge.conf
openssl x509 -req -in certs/edge.csr -CA certs/ca.crt -CAkey certs/ca.key \
    -CAcreateserial -out certs/edge.crt -days 365 \
    -extensions v3_req -extfile certs/edge.conf

# Clean up temporary files
rm certs/*.csr certs/*.srl certs/*.conf
```

**Important:** The certificates generated by the script include SANs (Subject Alternative Names) which are required for proper hostname verification with Go 1.25+ and modern TLS implementations.

## Password Authentication

When `allow_anonymous false` is set in `mosquitto.conf`, you need to set up username/password authentication.

### Generate Password File

Use the provided script to generate a password file:

```bash
cd mosquitto
./generate-passwd.sh
```

This will:
- Create a password file at `config/passwd`
- Create a user (default: `device-service`) with a password
- Display the credentials for use in docker-compose

You can customize the username and password via environment variables:

```bash
MQTT_USERNAME=myuser MQTT_PASSWORD=mypassword ./generate-passwd.sh
```

If no password is provided, a random password will be generated.

### Configure Device Service

Set the credentials in your `docker-compose.test.yml` or as environment variables:

```yaml
environment:
  - MQTT_USERNAME=device-service
  - MQTT_PASSWORD=your-password-here
```

The device service will automatically use these credentials when connecting to the MQTT broker.

### Manual Password Management

To manually create or add users to the password file:

```bash
# Create new password file (overwrites existing)
mosquitto_passwd -c config/passwd username

# Add user to existing password file
mosquitto_passwd config/passwd username

# Remove user from password file
mosquitto_passwd -D config/passwd username
```

**Note:** The password file is gitignored for security. Make sure to back it up or regenerate it as needed.

## Configuration

The Mosquitto configuration is managed via `docker-compose.test.yml` environment variables:

- `MQTT_PORT` - Non-TLS port (default: 1883)
- `MQTT_TLS_PORT` - TLS port (default: 8883)
- `MQTT_USERNAME` - Username for authentication (required if `allow_anonymous false`)
- `MQTT_PASSWORD` - Password for authentication (required if `allow_anonymous false`)
- `MQTT_TLS_CERT_FILE` - Server certificate path
- `MQTT_TLS_KEY_FILE` - Server key path
- `MQTT_TLS_CA_FILE` - CA certificate path
- `MQTT_REQUIRE_CERTIFICATE` - Require client certificates (default: false)
- `MQTT_ALLOW_ANONYMOUS` - Allow anonymous connections (default: true)

