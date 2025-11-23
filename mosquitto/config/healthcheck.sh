#!/bin/sh
# Healthcheck script for Mosquitto MQTT broker
# Tests TLS connection on port 8883, optionally with credentials

CA_FILE="/mosquitto/certs/ca.crt"
USERNAME="${MQTT_USERNAME:-}"
PASSWORD="${MQTT_PASSWORD:-}"

# Build mosquitto_sub command
CMD="mosquitto_sub -h 127.0.0.1 -p 8883 --cafile $CA_FILE -t 'test/health' -W 2 -C 1 -i healthcheck"

# Add credentials if provided
if [ -n "$USERNAME" ]; then
    CMD="$CMD -u $USERNAME"
    if [ -n "$PASSWORD" ]; then
        CMD="$CMD -P $PASSWORD"
    fi
fi

# Execute command and check for connection refused
OUTPUT=$(eval "$CMD" 2>&1)
if echo "$OUTPUT" | grep -q "Connection refused"; then
    exit 1
fi

# Any other result (success, auth error, etc.) means broker is accepting connections
exit 0

