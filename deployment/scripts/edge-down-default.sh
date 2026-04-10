#!/usr/bin/env bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
DEPLOYMENT_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"

cd "${DEPLOYMENT_DIR}"

docker compose -f docker-compose.evaluation.edge.yml down "$@"
