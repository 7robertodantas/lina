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

3. Mount the SSD at `/var/lib/lina`. Choose one of the following.

   **A. `/etc/fstab` (persists across reboots)**

   Get the partition UUID (`blkid` only prints identifiers; it does not mount).
   Replace `/dev/sda1` with your SSD if needed (e.g. NVMe):

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

   **B. Manual `mount` only (no `/etc/fstab`)**

   Use this for testing, one-off attach, or when something else manages mounts.
   The filesystem is **not** remounted automatically after reboot; repeat the
   `mount` after boot or switch to option A.

```bash
# By device node (adjust to your SSD):
sudo mount -t ext4 /dev/sda1 /var/lib/lina

# Or by UUID (stable if device names change):
sudo mount -t ext4 UUID=<your-ssd-uuid> /var/lib/lina
```

   Replace `ext4` if your partition uses another type (`blkid` shows `TYPE=`).

   To **unmount**, stop LINA services first so nothing keeps files open under
   `/var/lib/lina`:

```bash
sudo systemctl stop device-service ledger-service consumption-service lightning-service
sudo umount /var/lib/lina
```

   If `umount` reports that the target is busy, check what is using the path
   (`sudo lsof +f -- /var/lib/lina` or `findmnt /var/lib/lina`), stop it, then
   retry.

   **C. Only a subfolder on the SSD (`…/lina` → `/var/lib/lina`)**

   A block device mount always attaches the **whole filesystem** to one mount
   point. To use a directory on that disk (for example `/mnt/ssd/lina`) as
   `/var/lib/lina`, mount the partition somewhere else first, then **bind
   mount** the subdirectory.

   Staging mount point `/mnt/ssd` is an example; pick any empty directory.

   **Persist (`/etc/fstab`)** — list the data partition first, then the bind
   (order matters):

```fstab
UUID=<your-ssd-uuid>  /mnt/ssd  ext4  defaults,nofail  0  2
/mnt/ssd/lina         /var/lib/lina  none  bind,nofail  0  0
```

   Create the directory on the SSD once (after the partition exists and is
   mounted):

```bash
sudo mkdir -p /mnt/ssd
sudo mount -t ext4 UUID=<your-ssd-uuid> /mnt/ssd
sudo mkdir -p /mnt/ssd/lina
sudo umount /mnt/ssd
```

   Then `sudo mount -a` after editing `fstab`, or mount both entries by hand.

   **Manual (no `fstab`)** — create `lina` **on the SSD** after the partition is
   mounted (do not `mkdir /mnt/ssd/lina` before `mount`, or it may land on the
   root disk):

```bash
sudo mkdir -p /mnt/ssd
sudo mount -t ext4 UUID=<your-ssd-uuid> /mnt/ssd
sudo mkdir -p /mnt/ssd/lina
sudo mount --bind /mnt/ssd/lina /var/lib/lina
```

   **Unmount** (services stopped first): unmount the bind target, then the
   staging mount:

```bash
sudo umount /var/lib/lina
sudo umount /mnt/ssd
```

4. Ensure base permissions:

```bash
sudo chown root:root /var/lib/lina
sudo chmod 0755 /var/lib/lina
```

5. Re-run Ansible to recreate service directories and ownership (users under
   `/var/lib/lina` and per-service subtrees). You only need the LINA users/dirs
   tasks — not a full `site.yml`:

```bash
# From deployment/ansible/ (uses ansible.cfg roles_path):
ansible-playbook playbooks/ssd_recreate_lina_dirs.yml

# From repo root:
ANSIBLE_CONFIG=deployment/ansible/ansible.cfg ansible-playbook deployment/ansible/playbooks/ssd_recreate_lina_dirs.yml -i deployment/ansible/inventory/hosts
```

   To run the entire edge stack (same as a fresh deploy), use `playbooks/site.yml`
   instead.

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
