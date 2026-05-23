#!/bin/sh
set -e

# Remote scrape host (services/exporters on another machine or on the Docker host).
if [ -z "$PROMETHEUS_TARGET_HOST" ]; then
  echo "Warning: PROMETHEUS_TARGET_HOST not set, using 'localhost'"
  export PROMETHEUS_TARGET_HOST=localhost
fi

: "${PROMETHEUS_CLUSTER:=lina}"

# Self-scrape: Prometheus HTTP endpoint inside this container (host:port or [ipv6]:port).
: "${PROMETHEUS_SELF_SCRAPE_TARGET:=localhost:9090}"

# Comma-separated job_name:port (no spaces). Override fully in compose or Ansible.
# monitoring.json uses app metrics plus redis, node, and cAdvisor exporter families.
PROMETHEUS_SCRAPE_JOBS_DEFAULT='ledger:9460,consumption:9465,device:9466,redis:9461,node:9463,cadvisor:9462'
: "${PROMETHEUS_SCRAPE_JOBS:=${PROMETHEUS_SCRAPE_JOBS_DEFAULT}}"

substitute_cluster() {
  sed "s|\${PROMETHEUS_CLUSTER}|${PROMETHEUS_CLUSTER}|g" "$1"
}

# Emit one static_config job targeting PROMETHEUS_TARGET_HOST:port
append_remote_job() {
  _job=$1
  _port=$2
  printf "  - job_name: '%s'\n" "$_job"
  printf "    static_configs:\n"
  printf "      - targets: ['%s:%s']\n" "$PROMETHEUS_TARGET_HOST" "$_port"
}

{
  substitute_cluster /etc/prometheus/prometheus.template.yml
  printf '\nscrape_configs:\n'
  printf "  - job_name: 'prometheus'\n"
  printf "    static_configs:\n"
  printf "      - targets: ['%s']\n" "$PROMETHEUS_SELF_SCRAPE_TARGET"
  _old_ifs=$IFS
  IFS=','
  for _pair in $PROMETHEUS_SCRAPE_JOBS; do
    IFS=$_old_ifs
    _pair_trim=$(printf '%s' "$_pair" | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')
    [ -z "$_pair_trim" ] && continue
    _job=${_pair_trim%%:*}
    _port=${_pair_trim#*:}
    [ "$_job" = "$_pair_trim" ] && continue
    [ -z "$_job" ] || [ -z "$_port" ] && continue
    append_remote_job "$_job" "$_port"
  done
  IFS=$_old_ifs
} > /tmp/prometheus.yml

exec /bin/prometheus "$@"
