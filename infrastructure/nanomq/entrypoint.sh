#!/bin/sh
set -e

: "${DEVICE_SERVICE_MQTT_USERNAME:?DEVICE_SERVICE_MQTT_USERNAME is required}"
: "${DEVICE_SERVICE_HTTP_AUTH_URL:?DEVICE_SERVICE_HTTP_AUTH_URL is required}"

# Substitute placeholders in config template and write final config.
# @DEVICE_SERVICE_USERNAME@ is used in the static ACL rules.
# @DEVICE_SERVICE_HTTP_AUTH_URL@ is the base URL for NanoMQ's HTTP auth callbacks.
sed \
	-e "s|@DEVICE_SERVICE_USERNAME@|${DEVICE_SERVICE_MQTT_USERNAME}|g" \
	-e "s|@DEVICE_SERVICE_HTTP_AUTH_URL@|${DEVICE_SERVICE_HTTP_AUTH_URL}|g" \
	/etc/nanomq.conf.template > /etc/nanomq.conf

# Clean stale PID file from a previous run.
rm -f /tmp/nanomq/nanomq.pid

exec nanomq "$@"
