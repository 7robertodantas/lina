#!/bin/sh
# Entrypoint script for Mosquitto MQTT broker

# Generate password file (optional - only needed if password_file is enabled in mosquitto.conf)
# Uncomment the lines below if you need password_file authentication:
# /mosquitto/generate-passwd.sh
# if [ $? -ne 0 ]; then
#     echo "Failed to generate password file"
#     exit 1
# fi
# echo "Password file generated successfully"

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

# Start mosquitto with the provided command
exec "$@"

