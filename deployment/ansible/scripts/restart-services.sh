#!/usr/bin/env bash
# Run from anywhere: reload/restart edge services via playbooks/restart_services.yml
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
exec ansible-playbook playbooks/restart_services.yml "$@"
