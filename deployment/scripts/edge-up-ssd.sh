#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${DEPLOYMENT_DIR}"

# Load deployment/.env so EDGE_DATA_ROOT can be defined there.
if [[ -f .env ]]; then
  set -a
  # shellcheck disable=SC1091
  source .env
  set +a
fi

if [[ -z "${EDGE_DATA_ROOT:-}" ]]; then
  echo "ERROR: EDGE_DATA_ROOT is not set."
  echo "Set it in deployment/.env or export it in your shell."
  exit 1
fi

mkdir -p \
  "${EDGE_DATA_ROOT}/redis" \
  "${EDGE_DATA_ROOT}/mosquitto" \
  "${EDGE_DATA_ROOT}/device" \
  "${EDGE_DATA_ROOT}/ledger" \
  "${EDGE_DATA_ROOT}/consumption" \
  "${EDGE_DATA_ROOT}/lightning"

docker compose \
  -f docker-compose.evaluation.edge.yml \
  -f docker-compose.evaluation.edge.ssd.yml \
  up -d "$@"
