# Deployment Guide

This guide explains how to deploy the LNPay system to a new machine.

## Quick Answer: What Files Do I Need?

**To generate certificates on the new machine:**
- `docker-compose.prod.yml`
- `certs/generate-certs.sh` ← **Required**
- `.env` (or create one)
- `scripts/deploy.sh` (optional, for automated deployment)

**To use pre-generated certificates:**
- `docker-compose.prod.yml`
- `certs/ca.crt` (pre-generated)
- `certs/server.crt` (pre-generated)
- `certs/server.key` (pre-generated)
- `.env` (or create one)

**You do NOT need `certs/generate-certs.sh` if you're copying pre-generated certificates.**

## Quick Start (Simplest Method)

### Option 1: Automated Deployment Script (Recommended)

**For new machines, just run:**

```bash
# 1. Copy these files to the new machine:
#    - docker-compose.prod.yml
#    - scripts/deploy.sh
#    - certs/generate-certs.sh (required to generate certificates)
#    - .env (or create one)
# 2. Run:
./scripts/deploy.sh
```

The script will:
- ✅ Check for existing certificates
- ✅ Generate certificates if needed (requires `certs/generate-certs.sh`)
- ✅ Pull Docker images
- ✅ Start all services

**That's it!** The system will be up and running.

**Alternative:** If you already have certificates, just copy the certificate files (`ca.crt`, `server.crt`, `server.key`) to `./certs/` and the script will use them.

---

### Option 2: Manual Setup

**If generating certificates on the new machine:**

1. **Copy the certificate generation script:**
   ```bash
   # Copy certs/generate-certs.sh to the new machine
   ```

2. **Generate certificates:**
   ```bash
   ./certs/generate-certs.sh
   ```

3. **Start services:**
   ```bash
   docker-compose -f docker-compose.prod.yml up -d
   ```

**If using pre-generated certificates:**

1. **Copy certificate files to `./certs/` directory:**
   ```bash
   mkdir -p certs
   cp ca.crt server.crt server.key certs/
   ```

2. **Start services:**
   ```bash
   docker-compose -f docker-compose.prod.yml up -d
   ```

**Pros:**
- ✅ Simplest approach
- ✅ Certificates visible on host filesystem
- ✅ Easy to update certificates
- ✅ No volume management needed

**Cons:**
- Requires host filesystem access
- Certificates stored on host (ensure proper permissions)

---

## Detailed Deployment Steps

### Prerequisites

- Docker installed
- Docker Compose installed (or `docker compose` plugin)
- Network access to pull images from registry

### Step-by-Step Process

#### 1. Copy Required Files

**Option A: Generate certificates on new machine (recommended)**
```
docker-compose.prod.yml
.env (or create one with your configuration)
scripts/deploy.sh (optional, for automated deployment)
certs/generate-certs.sh (required to generate certificates)
```

**Option B: Use pre-generated certificates**
```
docker-compose.prod.yml
.env (or create one with your configuration)
certs/ca.crt (pre-generated)
certs/server.crt (pre-generated)
certs/server.key (pre-generated)
```

**Note:** If using Option B, you don't need `certs/generate-certs.sh`. Just copy the certificate files.

#### 2. Certificate Setup

**Option A: Use deploy.sh (Automated)**
```bash
./scripts/deploy.sh
```

**Option B: Manual Setup**
```bash
# Generate certificates
./certs/generate-certs.sh

# Start services
docker-compose -f docker-compose.prod.yml up -d
```

#### 3. Verify Deployment

```bash
# Check service status
docker-compose -f docker-compose.prod.yml ps

# Check logs
docker-compose -f docker-compose.prod.yml logs -f

# Test MQTT connection
docker exec mosquitto mosquitto_sub -h localhost -p 8883 --cafile /mosquitto/certs/ca.crt -t test -C 1
```

---

## Certificate Management

### First-Time Setup

**Option 1: Generate on new machine (requires `certs/generate-certs.sh`)**
```bash
./certs/generate-certs.sh
```

**Option 2: Copy pre-generated certificates**
```bash
mkdir -p certs
cp ca.crt server.crt server.key certs/
chmod 644 certs/*.crt
chmod 600 certs/*.key
```

### Using Existing Certificates

If you have certificates from another deployment:

```bash
# Copy certificates to the certs directory
cp ca.crt server.crt server.key ./certs/
```

### Certificate Rotation

```bash
# Replace certificates
cp new-ca.crt new-server.crt new-server.key ./certs/

# Restart services
docker-compose -f docker-compose.prod.yml restart mosquitto device smartmeter
```

---

## Environment Configuration

### Required Environment Variables

Create a `.env` file with your configuration:

```bash
# MQTT Configuration
MQTT_TLS_PORT=8883
DEVICE_SERVICE_MQTT_USERNAME=device-service
DEVICE_SERVICE_MQTT_PASSWORD=your-secure-password

# gRPC Port
GRPC_PORT=9090

# Redis
REDIS_PORT=6379

# Add other required variables...
```

### Optional: Override Certificate Generation

To use your organization's CA certificates, see `mosquitto/PRODUCTION_CERTS.md`.

---

## Troubleshooting

### Services won't start - "Required certificates not found"

**Solution:**
- Ensure certificates are generated: `./certs/generate-certs.sh`
- Verify certificates exist in `./certs/` directory
- Check file permissions

### Certificate errors in device service

**Solution:**
- Ensure device service can access `ca.crt`
- Check volume mount is correct
- Verify certificates are valid

### Port conflicts

**Solution:**
- Check if ports are already in use: `netstat -tulpn | grep -E '8080|8883|6379'`
- Update port mappings in `docker-compose.prod.yml` if needed

---

## Security Considerations

1. **File Permissions:**
   ```bash
   chmod 644 certs/*.crt
   chmod 600 certs/*.key
   ```

2. **Backup Certificates:**
   - Always backup certificates before deployment
   - Store backups securely

3. **Production:**
   - Use organization CA certificates when possible
   - Rotate certificates regularly
   - Monitor certificate expiration

---

## Minimal Deployment (Just docker-compose.prod.yml)

If you only want to copy `docker-compose.prod.yml`, you have two options:

**Option A: Generate certificates on new machine**
```bash
# 1. Copy certs/generate-certs.sh to new machine
# 2. Generate certificates
./certs/generate-certs.sh

# 3. Start services
docker-compose -f docker-compose.prod.yml up -d
```

**Option B: Use pre-generated certificates**
```bash
# 1. Copy certificate files to ./certs/ directory:
mkdir -p certs
cp ca.crt server.crt server.key certs/

# 2. Start services
docker-compose -f docker-compose.prod.yml up -d
```

**Note:** Certificates must be present before starting services. The entrypoint will fail if certificates are missing.

