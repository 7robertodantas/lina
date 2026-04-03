# Multipass edge VM — check services and logs (after Ansible)

Use this after `ansible-playbook playbooks/site.yml` against a Multipass (or any SSH) edge host. Everything runs under **systemd**; LINA apps and most dependencies log to **journald** (not separate log files under `/var/log` by default).

## Connect to the VM

**Option A — Multipass shell** (default VM name from `create-vm.sh` is `lina-edge-test`):

```bash
multipass shell "${MULTIPASS_VM_NAME:-lina-edge-test}"
```

**Option B — SSH** (match `ansible_host` and `ansible_user` in `deployment/ansible/inventory/hosts`):

```bash
multipass list   # copy IPv4 if needed
ssh ubuntu@<vm-ip>
```

## Which services should be running

| Unit | Role / purpose |
|------|----------------|
| `redis-server` | Redis |
| `mosquitto` | MQTT broker |
| `ledger-service` | LINA ledger |
| `lightning-service` | LINA lightning |
| `consumption-service` | LINA consumption |
| `device-service` | LINA device |
| `prometheus-node-exporter` | Host metrics (port 9100) |
| `process-exporter` | Process metrics (default `:9256`) |
| `redis-exporter` | Redis metrics (default `:9121`) |
| `systemd-exporter` | systemd metrics (default `:9558`; skipped if `lina_install_systemd_exporter` is false) |

Env files live under `/etc/lina/`; binaries under `/opt/lina/bin/`. See `deployment/ansible/README.md` for ports and TLS notes.

## Check that services are running

**Quick overview — anything failed:**

```bash
systemctl --failed
```

**Status of the whole stack (one command):**

```bash
systemctl status redis-server mosquitto \
  ledger-service lightning-service consumption-service device-service \
  prometheus-node-exporter process-exporter redis-exporter systemd-exporter \
  --no-pager
```

If `systemd-exporter` was not installed, that line may show “could not be found”; that is expected when `lina_install_systemd_exporter` is false.

**Filter running units:**

```bash
systemctl list-units --type=service --state=running \
  | grep -E 'redis|mosquitto|ledger|lightning|consumption|device|exporter'
```

**Is a specific service active?**

```bash
systemctl is-active ledger-service
```

**Recent start/stop history:**

```bash
journalctl -u device-service --since "1 hour ago" --no-pager
```

## View logs

LINA systemd units do not set a custom `StandardOutput=`; stdout/stderr go to the **journal** for each unit.

**Follow live logs for one service:**

```bash
sudo journalctl -u ledger-service -f
```

**Last 200 lines, then follow:**

```bash
sudo journalctl -u consumption-service -n 200 -f
```

**Time window:**

```bash
sudo journalctl -u device-service --since today --until now
sudo journalctl -u mosquitto --since "2026-04-03 10:00" --until "2026-04-03 11:00"
```

**All LINA app units together (no follow):**

```bash
sudo journalctl -u ledger-service -u lightning-service \
  -u consumption-service -u device-service -n 100 --no-pager
```

**Broker and Redis:**

```bash
sudo journalctl -u mosquitto -f
sudo journalctl -u redis-server -f
```

**Boot-time errors across the box:**

```bash
sudo journalctl -b -p err..alert --no-pager
```

Use `sudo` if your user cannot read other units’ journals (Ubuntu often allows your own user only for some commands).

## From your Mac (optional, no SSH shell)

Using the same inventory as Ansible:

```bash
cd deployment/ansible

ansible edge -m shell -a "systemctl is-active redis-server mosquitto ledger-service lightning-service consumption-service device-service" -b

ansible edge -m shell -a "journalctl -u device-service -n 50 --no-pager" -b
```

`-b` becomes root on the target (sudo), which matches typical journal access needs.

## If something is not running

```bash
sudo systemctl restart ledger-service
sudo journalctl -u ledger-service -n 100 --no-pager
```

For config changes under `/etc/lina/`, reload or restart the affected unit after editing; prefer editing via Ansible templates on the controller and re-running the playbook so prod and dev stay aligned.
