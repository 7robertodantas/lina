#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${DEPLOYMENT_DIR}"

if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

EDGE_ROOT="${EDGE_DATA_ROOT:-.data/edge}"
mkdir -p \
  "${EDGE_ROOT}/redis" \
  "${EDGE_ROOT}/mosquitto" \
  "${EDGE_ROOT}/device" \
  "${EDGE_ROOT}/ledger" \
  "${EDGE_ROOT}/consumption" \
  "${EDGE_ROOT}/lightning"

docker compose -f docker-compose.edge.yml up -d "$@"
