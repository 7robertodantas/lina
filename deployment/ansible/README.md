# Native edge deployment (Ansible)

Installs the LINA edge stack on a Debian-based host **without Docker**: `redis-server`, `mosquitto`, four Go services under systemd, and Prometheus-style exporters (node, process, redis, optional systemd).

Layout matches `ansible.md` at the repo root (`/opt/lina/bin`, `/etc/lina`, `/var/lib/lina`, journald logging).

## Prerequisites

- Control machine: Ansible 2.14+ (`ansible-playbook`).
- Control machine: **Go** (e.g. `brew install go`) so the playbook can cross-compile services by default.
- Target: Debian 12+ or Raspberry Pi OS (64-bit), sudo/root over SSH.

By default the **`lina-services`** role runs `go build` on the **controller** for `GOOS`/`GOARCH` from `inventory/group_vars` (`linux` + `arm64` for Pi / Multipass) and writes to `deployment/ansible/.build/<goos>-<goarch>/`. Set **`lina_build_binaries: false`** and **`lina_binaries_dir`** if you prefer to copy pre-built binaries instead.

On **Apple Silicon**, a local **Ubuntu arm64 Multipass** VM is a convenient target (same arch as 64-bit Pi). See `deployment/multipass/README.md` and `deployment/multipass/create-vm.sh`.

## Configure

1. Edit `inventory/hosts` (or copy from `inventory/hosts.example`): set `ansible_host` and `ansible_user`. Override `lina_build_goarch` / `lina_build_goos` in `inventory/group_vars/all.yml` if the target differs.
2. Edit `inventory/group_vars/all.yml` for MQTT credentials, `SERVICE_TOKEN`, and **Lightning** (`lina_lnd_*` hex values). Use `ansible-vault` for production secrets.

Ports used on a single host are split to avoid collisions (REST, gRPC, and `METRICS_ADDR`); adjust variables under “Single-host ports” in `inventory/group_vars/all.yml` if needed.

## Run

From this directory (`deployment/ansible`):

```bash
ansible-playbook playbooks/site.yml
```

Ansible loads `ansible.cfg` here, including `inventory/hosts` and `roles/`.

## After deploy

- **Northbound HTTP** via **Caddy** on **`8080`** by default (same path rules as `infrastructure/caddy/Caddyfile`: `/devices*`, ledger/consumption/lightning routes, `/health`). Override `lina_caddy_listen` / `lina_caddy_admin` in `inventory/group_vars/all.yml`.
- Application metrics: device `9466`, ledger `9460`, consumption `9465` (defaults; overridable via `METRICS_ADDR` in each env file template).
- Node exporter (package): port **9463** (`lina_node_exporter_listen`; matches Docker host mapping `9463:9100`).
- Process exporter: **9256** (`lina_process_exporter_listen`; same as `docker-compose.edge.yml`).
- Redis exporter: **9461** (`lina_redis_exporter_listen`; matches Docker `9461:9121`).
- Systemd exporter (optional): **9558** (Linux host / D-Bus; same as evaluation edge compose when enabled).

**TLS** for Mosquitto is **on** by default (`8883`): Ansible copies `ca.crt`, `server.crt`, and `server.key` from **`infrastructure/certs`** on the controller (run `infrastructure/certs/generate-certs.sh` first) or from **`lina_mosquitto_certs_src`** if you set it. Plain MQTT is on **1883**. With **`lina_mosquitto_dynsec_enable: true`** (default), the broker loads **`mosquitto_dynamic_security.so`**, keeps **`allow_anonymous false`**, and initializes **`/var/lib/mosquitto/dynamic-security.json`** via `mosquitto_ctrl dynsec init` using **`lina_mqtt_dynsec_admin_user`** / **`lina_mqtt_dynsec_admin_password`** (must match device-service). WebSocket listeners default to **9001** (plain) and **9002** (TLS when TLS is enabled), like `infrastructure/mosquitto/config/mosquitto.conf`. Set **`lina_mosquitto_dynsec_enable: false`** only for lab anonymous access. For real certificates, set `lina_mqtt_tls_skip_verify: false` and `lina_mqtt_tls_server_name` as needed in `inventory/group_vars/all.yml`.

The **mosquitto** role comments out stock `listener` / deprecated `port` lines and `persistence` / `persistence_location` in `/etc/mosquitto/mosquitto.conf` and in other `conf.d/*.conf` files (except `99-lina.conf`), then defines listeners and persistence only in `99-lina.conf`. If the broker still fails to start, run `journalctl -xeu mosquitto.service` on the target.

## Roles

| Role           | Purpose                                                |
|----------------|--------------------------------------------------------|
| `common`       | apt update, base packages                              |
| `redis`        | `redis-server`, loopback bind, optional password       |
| `mosquitto`    | broker + `/etc/mosquitto/conf.d/99-lina.conf`          |
| `lina-services`| users, binaries, `/etc/lina/*.env`, systemd units      |
| `monitoring`   | `prometheus-node-exporter` (apt), process/redis/systemd exporters |
