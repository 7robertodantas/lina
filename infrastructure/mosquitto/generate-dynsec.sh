#!/bin/sh
# Set Default Admin Credentials for Dynamic Security Plugin Configuration
DEFAULT_DYNSEC_ADMIN=admin
DEFAULT_DYNSEC_ADMIN_PASSWORD=admin

# Set values if provided via Environment Variables in the Docker Init Container
MQTT_DYNSEC_ADMIN_USER=${MQTT_DYNSEC_ADMIN_USER:-$DEFAULT_DYNSEC_ADMIN}
MQTT_DYNSEC_ADMIN_PASSWORD=${MQTT_DYNSEC_ADMIN_PASSWORD:-$DEFAULT_DYNSEC_ADMIN_PASSWORD}

# Check the mode
MODE=${1:-full}

if [ "$MODE" = "init-only" ]; then
    # Only initialize the dynamic-security.json file (doesn't need a running broker)
    echo "Initializing dynamic-security.json file..."
    mosquitto_ctrl dynsec init /mosquitto/data/dynamic-security.json ${MQTT_DYNSEC_ADMIN_USER} ${MQTT_DYNSEC_ADMIN_PASSWORD}
    exit $?
fi

# Configure mode - requires a running broker
if [ "$MODE" != "configure" ] && [ "$MODE" != "full" ]; then
    echo "Unknown mode: $MODE"
    exit 1
fi

# If full mode, initialize first
if [ "$MODE" = "full" ]; then
    echo "Initializing dynamic-security.json file..."
    mosquitto_ctrl dynsec init /mosquitto/data/dynamic-security.json ${MQTT_DYNSEC_ADMIN_USER} ${MQTT_DYNSEC_ADMIN_PASSWORD}
    if [ $? -ne 0 ]; then
        echo "Failed to initialize dynamic-security.json"
        exit 1
    fi
fi

# Connection options for mosquitto_ctrl (using TLS on localhost)
CTRL_OPTS="-h 127.0.0.1 -p 8883 --cafile /mosquitto/certs/ca.crt -u ${MQTT_DYNSEC_ADMIN_USER} -P ${MQTT_DYNSEC_ADMIN_PASSWORD}"

echo "Dynamic security configuration completed successfully"
