#!/bin/sh
# Entrypoint script for Mosquitto MQTT broker

# Certificate validation - certificates must be provided
CERT_DIR="/mosquitto/certs"

# Check if certificates exist
if [ ! -f "$CERT_DIR/ca.crt" ] || [ ! -f "$CERT_DIR/server.crt" ] || [ ! -f "$CERT_DIR/server.key" ]; then
    echo "ERROR: Required certificates not found in $CERT_DIR"
    echo ""
    echo "Missing certificates:"
    [ ! -f "$CERT_DIR/ca.crt" ] && echo "  - ca.crt"
    [ ! -f "$CERT_DIR/server.crt" ] && echo "  - server.crt"
    [ ! -f "$CERT_DIR/server.key" ] && echo "  - server.key"
    echo ""
    echo "Please provide certificates via volume mount."
    echo "Generate certificates using: ./certs/generate-certs.sh"
    echo ""
    exit 1
fi

echo "Certificates found, using provided certificates"

# Fix certificate file permissions so mosquitto user can read them
# Note: This requires the volume to be mounted as read-write (not :ro)
# The mosquitto user (typically UID 1883) needs read access to server.key
if [ -f "$CERT_DIR/server.key" ]; then
    # Check if we're running as root (entrypoint should run as root initially)
    if [ "$(id -u)" = "0" ]; then
        # We're root, so we can fix permissions
        if id mosquitto >/dev/null 2>&1; then
            chown mosquitto:mosquitto "$CERT_DIR/server.key" 2>/dev/null || echo "WARNING: Could not chown server.key"
            chown mosquitto:mosquitto "$CERT_DIR/server.crt" 2>/dev/null || echo "WARNING: Could not chown server.crt"
            chown mosquitto:mosquitto "$CERT_DIR/ca.crt" 2>/dev/null || echo "WARNING: Could not chown ca.crt"
        fi
        # Set permissions: 644 for all files (readable by all, including mosquitto user)
    # Using 644 instead of 600 for server.key ensures mosquitto can read it even if UID doesn't match
    # This is acceptable since the volume is only accessible within the container
        chmod 644 "$CERT_DIR/ca.crt" 2>/dev/null || echo "WARNING: Could not chmod ca.crt"
        chmod 644 "$CERT_DIR/server.crt" 2>/dev/null || echo "WARNING: Could not chmod server.crt"
        chmod 644 "$CERT_DIR/server.key" 2>/dev/null || echo "WARNING: Could not chmod server.key"
        echo "Certificate permissions set"
    else
        # Not root - check if permissions are already correct
        if [ ! -r "$CERT_DIR/server.key" ]; then
            echo "WARNING: Cannot read server.key and not running as root to fix permissions"
            echo "Please ensure certificates have correct permissions on the host:"
            echo "  chmod 644 $CERT_DIR/*.crt"
            echo "  chmod 600 $CERT_DIR/server.key"
            echo "  chown mosquitto:mosquitto $CERT_DIR/* (or ensure files are world-readable)"
        fi
    fi
fi

# Runs generate-dynsec.sh on first startup if dynamic-security.json doesn't exist

DYNSEC_FILE="/mosquitto/data/dynamic-security.json"
DYNSEC_DIR="/mosquitto/data"

# Ensure data directory exists and has correct permissions
mkdir -p "$DYNSEC_DIR"
chmod 755 "$DYNSEC_DIR"

# Check if dynamic-security.json exists, if not, initialize it
if [ ! -f "$DYNSEC_FILE" ]; then
    echo "dynamic-security.json not found. Initializing it..."
    
    # Initialize the dynamic-security.json file (this doesn't need a running broker)
    /mosquitto/generate-dynsec.sh init-only
    if [ $? -ne 0 ]; then
        echo "Failed to initialize dynamic-security.json"
        exit 1
    fi
    
    # Fix permissions so mosquitto can read the file
    # Security requirement: file must be 0700 (readable/writable only by owner)
    # Try to set ownership to mosquitto user if it exists, otherwise just set permissions
    if id mosquitto >/dev/null 2>&1; then
        chown mosquitto:mosquitto "$DYNSEC_FILE" 2>/dev/null || true
        chown mosquitto:mosquitto "$DYNSEC_DIR" 2>/dev/null || true
    fi
    chmod 0700 "$DYNSEC_FILE"
    chmod 755 "$DYNSEC_DIR"
    
    echo "dynamic-security.json initialized successfully"
    echo "Note: Device service will configure users and roles on startup"
else
    echo "dynamic-security.json already exists, skipping initialization"
fi

# Switch to mosquitto user before starting mosquitto (if we're root)
# The base image typically runs as mosquitto user, but if we're root we should switch
if [ "$(id -u)" = "0" ] && id mosquitto >/dev/null 2>&1; then
    # Try su-exec (Alpine)
    if command -v su-exec >/dev/null 2>&1; then
        exec su-exec mosquitto "$@"
    # Try gosu (Debian-based)
    elif command -v gosu >/dev/null 2>&1; then
        exec gosu mosquitto "$@"
    else
        # Fallback: use su (less ideal but works)
        exec su -s /bin/sh mosquitto -c "$*"
    fi
else
    # Already running as mosquitto or mosquitto user doesn't exist, just exec
    # The base image should handle user switching if needed
    exec "$@"
fi

