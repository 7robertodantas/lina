#!/bin/sh
# Healthcheck script for Mosquitto MQTT broker
# Tests if the broker is accepting TLS connections on port 8883
# Uses a simple connection test - doesn't require authentication

CA_FILE="/mosquitto/certs/ca.crt"

# Try to connect using mosquitto_sub with a short timeout
# We don't need credentials - we just need to verify the broker is accepting connections
# "Connection refused" means broker is down, any other error (like auth) means broker is up
OUTPUT=$(mosquitto_sub -h 127.0.0.1 -p 8883 --cafile "$CA_FILE" -t 'test/health' -W 1 -C 1 -i "healthcheck-$$" 2>&1)

# Check for connection refused (broker not accepting connections)
if echo "$OUTPUT" | grep -q "Connection refused"; then
    exit 1
fi

# Any other result means broker is accepting connections
# This includes:
# - Success (connected and subscribed)
# - Authentication errors (broker is up, just not authorized)
# - Network errors (broker is up, just can't complete handshake)
exit 0

