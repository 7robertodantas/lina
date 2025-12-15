#!/bin/sh
set -e

# Substitute TARGET_HOST environment variable in prometheus.yml template
if [ -z "$TARGET_HOST" ]; then
  echo "Warning: TARGET_HOST not set, using 'localhost'"
  export TARGET_HOST=localhost
fi

# Use sed to replace ${TARGET_HOST} with the actual value
sed "s|\${TARGET_HOST}|${TARGET_HOST}|g" /etc/prometheus/prometheus.template.yml > /etc/prometheus/prometheus.yml

# Execute the original Prometheus entrypoint
exec /bin/prometheus "$@"
