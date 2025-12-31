# Mosquitto MQTT Broker Configuration

This directory contains the configuration for the Eclipse Mosquitto MQTT broker.

## Directory Structure

- `config/` - Mosquitto configuration files
- `data/` - Persistence data (gitignored)
- `log/` - Log files (gitignored)

## TLS Certificates

Certificates are managed centrally in the project root `certs/` directory and mounted into containers.

### Certificate Location

Certificates are stored in the project root `certs/` directory:
- `./certs/ca.crt` - Certificate Authority certificate (public)
- `./certs/ca.key` - Certificate Authority private key (keep secure!)
- `./certs/server.crt` - Server certificate (public)
- `./certs/server.key` - Server private key (keep secure!)

### Certificate Usage

**Mosquitto Service:**
- Requires: `ca.crt`, `server.crt`, `server.key`
- Mounted as: `./certs:/mosquitto/certs:ro`
- Uses certificates for TLS server authentication

**Client Services (device, smartmeter):**
- Requires: `ca.crt` only (public certificate for server verification)
- Mounted as: `./certs/ca.crt:/certs/ca.crt:ro`
- **Note:** Private keys are NOT mounted to client services for security

**Current Configuration:**
- Mosquitto uses: `ca.crt`, `server.crt`, `server.key` (configured in `mosquitto.conf`)
- Client services use: `ca.crt` only (to verify the server's certificate)
- Authentication: Username/password via Dynamic Security Plugin (mutual TLS not used)

## Generating Certificates

### Recommended: Use the generation script

The easiest way to generate certificates is using the provided script in the project root:

```bash
./certs/generate-certs.sh
```

This script will:
- Check if certificates already exist (won't overwrite existing ones)
- Generate CA and server certificates if missing
- Include Subject Alternative Names (SANs) in certificates for proper hostname verification
- Set appropriate validity periods (CA: 10 years, Server: 1 year by default)
- Clean up temporary files automatically

**Note:** If you have existing certificates without SANs, you'll need to delete them first to regenerate:
```bash
rm -f certs/server.crt certs/server.key
./certs/generate-certs.sh
```

**Custom Validity Periods:**
```bash
CA_VALIDITY_DAYS=7300 SERVER_VALIDITY_DAYS=730 ./certs/generate-certs.sh
```

### Using Your Organization's CA

If you have an existing CA, place your certificates in the `certs/` directory:
- `ca.crt` - Your organization's CA certificate
- `server.crt` - Server certificate signed by your CA
- `server.key` - Server private key

The entrypoint will use these certificates if they exist. See `certs/README.md` for more details.

**Important:** Certificates must include Subject Alternative Names (SANs) for proper hostname verification with Go 1.25+ and modern TLS implementations.

## Authentication

This setup uses **Dynamic Security Plugin** for authentication (configured in `mosquitto.conf`). The password file authentication is disabled.

### Dynamic Security Plugin

The Dynamic Security Plugin is initialized automatically on first startup via the entrypoint script. The device service will configure users and roles dynamically.

### Configure Device Service

Set the credentials in your `docker-compose.development.yml` or as environment variables:

```yaml
environment:
  - MQTT_USERNAME=device-service
  - MQTT_PASSWORD=your-password-here
```

The device service will automatically use these credentials when connecting to the MQTT broker and will configure them in Dynamic Security if needed.

## Configuration

The Mosquitto configuration is managed via environment variables in `docker-compose.development.yml`:

- `MQTT_TLS_PORT` - TLS port (default: 8883)
- `MQTT_USERNAME` - Username for authentication (required, `allow_anonymous false`)
- `MQTT_PASSWORD` - Password for authentication (required, `allow_anonymous false`)

### Certificate Requirements

Certificates must be provided before starting the container:
- Certificates are mounted from `./certs` directory
- The entrypoint will fail if certificates are missing
- Generate certificates using: `./certs/generate-certs.sh`

### Security Notes

- Private keys (`*.key`) are only accessible to the mosquitto service
- Client services only receive `ca.crt` (public certificate)
- Certificates are mounted read-only (`:ro`) for security
- Never commit private keys to version control

