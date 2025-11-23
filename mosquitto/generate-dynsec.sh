#!/bin/sh
# Set Default Admin Credentials for Dynamic Security Plugin Configuration
DEFAULT_DYNSEC_ADMIN=admin
DEFAULT_DYNSEC_PASSWORD=admin

# Set values if provided via Environment Variables in the Docker Init Container
MQTT_DYNSEC_ADMIN_USER=${MQTT_DYNSEC_ADMIN_USER:-$DEFAULT_DYNSEC_ADMIN}
MQTT_DYNSEC_ADMIN_PASSWORD=${MQTT_DYNSEC_ADMIN_PASSWORD:-$DEFAULT_DYNSEC_ADMIN_PASSWORD}

# echo "Admin/Pass: ${MQTT_DYNSEC_ADMIN_USER}/${MQTT_DYNSEC_ADMIN_PASSWORD}" ## DEBUG

# Set the Admin Credentials for Dynamic Security Plugin
mosquitto_ctrl dynsec init /mosquitto/data/dynamic-security.json ${MQTT_DYNSEC_ADMIN_USER} ${MQTT_DYNSEC_ADMIN_PASSWORD}