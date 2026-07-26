# Mounts (host directories)

Share a host directory into a guest with virtio-9p (default) or virtiofs (Linux) at create time:

```bash
grain new -v /Users/me/src:/mnt/src
grain new -v .:/work -v /tmp/data:/data
```

## Syntax (`-v` / `--volume`)

```text
HOST:GUEST
```

| Side | Rules |
|------|--------|
| **HOST** | Directory path. May be `.` or relative; resolved to an absolute path. Must exist and be a directory. |
| **GUEST** | Absolute path inside the guest (must start with `/`). |

Repeatable. Example:

```bash
grain new -v ~/projects/app:/app -v /var/cache/build:/cache
```

On success, create output includes `vol=<host>→<guest>` for each share.

## How it works

1. **QEMU** exposes each host dir as a shared-fs device with a mount tag (`grain0`, `grain1`, …).
2. **Default driver is virtio-9p** (`security_model=mapped-xattr`) — macOS-friendly with QEMU HVF; not `passthrough`. File ownership/permissions are mapped via xattrs rather than applying host UIDs directly.
3. **cloud-init** injects first-boot `runcmd` entries that mkdir the guest path and mount:

```bash
# 9p (default)
mkdir -p /mnt/src && mount -t 9p -o trans=virtio,version=9p2000.L grain0 /mnt/src

# virtiofs (Linux host + mount_driver=virtiofs + virtiofsd)
mkdir -p /mnt/src && mount -t virtiofs grain0 /mnt/src
```

Mounts are stored in VM metadata. On `grain start`, grain re-validates that host dirs still exist and re-attaches the share devices. If the NoCloud seed is missing, it is regenerated including mount runcmds (first-boot only for new seeds; an already-booted guest may already have mounts or fstab—recreate if you need a clean first boot).

## Mount driver (`mount_driver`)

Config key (default `9p`):

```yaml
# ~/.grain/config.yaml
mount_driver: 9p        # default — works on macOS HVF and Linux
# mount_driver: virtiofs  # Linux + virtiofsd → real virtiofs; else falls back to 9p
```

| Driver | Status |
|--------|--------|
| **9p** | Default. Stable with QEMU on macOS (HVF) and Linux. Guest mounts with `mount -t 9p`. |
| **virtiofs** | On **Linux** hosts when `virtiofsd` is available (PATH or `/usr/libexec/virtiofsd`), grain starts one virtiofsd per mount, wires QEMU `vhost-user-fs-pci` + a shared `memory-backend-file`, and cloud-init uses `mount -t virtiofs`. On **darwin**, or when virtiofsd is missing, grain logs a warning and **falls back to 9p**. |

### virtiofs details (Linux)

When the effective driver is virtiofs:

1. For each `-v` mount, grain starts:
   ```text
   virtiofsd --socket-path=<vmDir>/virtiofsd-N.sock --shared-dir=<host> --cache=auto
   ```
   (unprivileged hosts also pass `--sandbox=none`.)
2. QEMU is given a shared memory object (required for vhost-user-fs) and one chardev/device pair per share:
   ```text
   -object memory-backend-file,id=mem,size=<MB>M,mem-path=/dev/shm,share=on
   -numa node,memdev=mem
   -chardev socket,id=charfsN,path=<vmDir>/virtiofsd-N.sock
   -device vhost-user-fs-pci,queue-size=1024,chardev=charfsN,tag=<tag>
   ```
3. On VM stop, grain kills the virtiofsd processes and removes their sockets/pidfiles under the VM directory.

Install virtiofsd from your distro (e.g. package `virtiofsd`) or [upstream](https://gitlab.com/virtio-fs/virtiofsd). The guest kernel must support `virtiofs` (common on recent Ubuntu/Fedora cloud images).

## Caveats

- **Create-time only** — you cannot add or remove `-v` mounts on a running VM; recreate with the desired flags.
- **Host path must remain a directory** — if you delete or replace the host dir, `start` fails until the path is a directory again.
- **9p performance** — fine for source and config; not ideal for heavy random I/O databases. Prefer guest disk for large writable datasets, or `mount_driver: virtiofs` on Linux.
- **Permissions** — 9p `mapped-xattr` can surprise tools that expect exact host UID/GID. Prefer world-readable host trees or run as the default `ubuntu` / `grain` user in the guest. virtiofsd maps UIDs differently (often closer to host when run unprivileged).
- **No file mounts** — only directories; host must be a dir at create (and at start).
- **macOS host paths** — absolute Unix paths work; host path splitting uses the first `:` (Unix hosts do not use drive letters).
- **virtiofs memory backend** — uses `/dev/shm` for the shared memory object; ensure enough tmpfs headroom for guest RAM size.

## Guest-only mounts via userdata

Besides `-v`, you can mount things from inside the guest with `--userdata-file` (e.g. NFS, extra fstab). That is independent of the host share driver. Example shell userdata:

```bash
# userdata.sh
#!/bin/sh
mkdir -p /opt/cache
# ... your mounts or package setup ...
```

```bash
grain new --userdata-file ./userdata.sh
```

Or a `#cloud-config` document with `runcmd` / `mounts` keys—grain merges list keys (`runcmd`, `packages`, …) with its base config. See [Images](images.md) for the default cloud image and SSH user.

## Related

- [Networking](networking.md) — SLIRP and `--publish`
- [Troubleshooting](troubleshooting.md) — cloud-init and serial logs
