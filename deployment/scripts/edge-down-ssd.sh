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

docker compose \
  -f docker-compose.evaluation.edge.yml \
  -f docker-compose.evaluation.edge.ssd.yml \
  down "$@"
