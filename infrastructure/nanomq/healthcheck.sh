#!/bin/sh
set -e
# Probe the NanoMQ management HTTP API (Basic auth on http_server — not MQTT).
# GET /api/v4/metrics: https://nanomq.io/docs/en/latest/api/v4.html
NANOMQ_HTTP_PORT="${NANOMQ_HTTP_PORT:-8081}"
NANOMQ_HTTP_USERNAME="${NANOMQ_HTTP_USERNAME:-admin}"
NANOMQ_HTTP_PASSWORD="${NANOMQ_HTTP_PASSWORD:-public}"
curl -sf -u "${NANOMQ_HTTP_USERNAME}:${NANOMQ_HTTP_PASSWORD}" \
	"http://127.0.0.1:${NANOMQ_HTTP_PORT}/api/v4/metrics" -o /dev/null
