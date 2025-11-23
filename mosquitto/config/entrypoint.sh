#!/bin/sh
# Entrypoint script for Mosquitto MQTT broker

# Generate password file
/mosquitto/generate-passwd.sh
if [ $? -ne 0 ]; then
    echo "Failed to generate password file"
    exit 1
fi

echo "Password file generated successfully"

# Runs generate-dynsec.sh on first startup if dynamic-security.json doesn't exist

DYNSEC_FILE="/mosquitto/data/dynamic-security.json"

# Check if dynamic-security.json exists, if not, generate it
if [ ! -f "$DYNSEC_FILE" ]; then
    echo "dynamic-security.json not found. Generating it..."
    /mosquitto/generate-dynsec.sh
    if [ $? -ne 0 ]; then
        echo "Failed to generate dynamic-security.json"
        exit 1
    fi
    echo "dynamic-security.json generated successfully"
else
    echo "dynamic-security.json already exists, skipping generation"
fi



# Start mosquitto with the provided command
exec "$@"

