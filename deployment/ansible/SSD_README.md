# SSD Storage Guide for Ansible Deployment

This guide explains how to switch LINA service data to an external SSD when
running the native Ansible + systemd deployment.

LINA service DB paths are under:

- `/var/lib/lina/ledger`
- `/var/lib/lina/consumption`
- `/var/lib/lina/lightning`
- `/var/lib/lina/device`

The Ansible role already uses `lina_lib_dir` (default: `/var/lib/lina`) for
these paths.

---

## Option A: Fresh Start on SSD (No Data Copy)

Use this when you do not need existing data.

1. Stop services:

```bash
sudo systemctl stop device-service ledger-service consumption-service lightning-service
```

2. Backup old dir (optional) and recreate mount target:

```bash
sudo mv /var/lib/lina /var/lib/lina.bak.$(date +%F-%H%M%S)
sudo mkdir -p /var/lib/lina
```

3. Mount SSD at `/var/lib/lina`:

```bash
sudo blkid /dev/sda1
```

Add to `/etc/fstab` (example for ext4):

```fstab
UUID=<your-ssd-uuid>  /var/lib/lina  ext4  defaults,nofail  0  2
```

Apply mounts:

```bash
sudo mount -a
```

4. Ensure base permissions:

```bash
sudo chown root:root /var/lib/lina
sudo chmod 0755 /var/lib/lina
```

5. Re-run Ansible to recreate service directories and ownership:

```bash
ansible-playbook deployment/ansible/playbooks/site.yml -i deployment/ansible/inventory/hosts
```

6. Start services:

```bash
sudo systemctl start ledger-service lightning-service consumption-service device-service
```

---

## Option B: Move Existing Data to SSD

Use this when you need to preserve current data.

1. Stop services:

```bash
sudo systemctl stop device-service ledger-service consumption-service lightning-service
```

2. Mount SSD temporarily and copy:

```bash
sudo mkdir -p /mnt/ssd
sudo mount /dev/sda1 /mnt/ssd
sudo mkdir -p /mnt/ssd/lina
sudo rsync -aHAX --info=progress2 /var/lib/lina/ /mnt/ssd/lina/
```

3. Backup old dir and prepare mount target:

```bash
sudo mv /var/lib/lina /var/lib/lina.pre-ssd-backup
sudo mkdir -p /var/lib/lina
```

4. Persist mount using one of these methods:

- Mount whole SSD partition at `/var/lib/lina` (if data is at partition root), or
- Use bind mount when data is in `/mnt/ssd/lina`:

```fstab
/mnt/ssd/lina  /var/lib/lina  none  bind  0  0
```

Apply:

```bash
sudo mount -a
```

5. Start services:

```bash
sudo systemctl start ledger-service lightning-service consumption-service device-service
```

---

## Validation

Check mount is active:

```bash
mount | rg "/var/lib/lina"
df -h /var/lib/lina
```

Check directory ownership:

```bash
ls -la /var/lib/lina
```

Check service health:

```bash
sudo systemctl status ledger-service lightning-service consumption-service device-service --no-pager
sudo journalctl -u ledger-service -u lightning-service -u consumption-service -u device-service -n 100 --no-pager
```

---

## Rollback

1. Stop services.
2. Remove/disable SSD mount from `/etc/fstab`.
3. Restore backup:

```bash
sudo umount /var/lib/lina || true
sudo rm -rf /var/lib/lina
sudo mv /var/lib/lina.pre-ssd-backup /var/lib/lina
```

4. Start services again.

---

## Notes

- This affects LINA service data only (`lina_lib_dir`).
- Redis and Mosquitto use distro defaults (`/var/lib/redis`, `/var/lib/mosquitto`).
  If you want them on SSD too, migrate/mount those paths separately.
