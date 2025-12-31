#!/bin/sh
set -e

# Substitute PROMETHEUS_TARGET_HOST environment variable in prometheus.yml template
if [ -z "$PROMETHEUS_TARGET_HOST" ]; then
  echo "Warning: PROMETHEUS_TARGET_HOST not set, using 'localhost'"
  export PROMETHEUS_TARGET_HOST=localhost
fi

# Generate prometheus.yml in a location that's not mounted (inside the container only)
# This prevents the file from being written to the local filesystem
# Using /tmp which is not mounted and is container-local
sed "s|\${PROMETHEUS_TARGET_HOST}|${PROMETHEUS_TARGET_HOST}|g" /etc/prometheus/prometheus.template.yml > /tmp/prometheus.yml

# Execute the original Prometheus entrypoint
exec /bin/prometheus "$@"
