# Base images

## Default: `ubuntu-cloud`

grain’s catalog ships **Ubuntu 24.04 minimal cloud** as `ubuntu-cloud` (architecture-specific URL):

- arm64 / amd64 builds from [Ubuntu cloud images](https://cloud-images.ubuntu.com/minimal/releases/noble/release/)
- qcow2 disk, **cloud-init** NoCloud, default SSH user **`ubuntu`**
- Sized for a full cloud stack (~300 MB download), not a minimal initramfs toy
- **HasAgent: false** — grain deploys `grain-agent` over SSH after first boot (`make agent-linux`)

```bash
grain image ls
grain image pull              # default id from config (ubuntu-cloud)
grain image pull ubuntu-cloud
```

Config default:

```yaml
image: ubuntu-cloud
ssh_user: ubuntu
```

Override per VM:

```bash
grain new -i ubuntu-cloud
```

## Golden image: `grain-ubuntu`

`grain-ubuntu` is a **local-only** catalog id: Ubuntu cloud + **grain-agent baked in**. There is no public download URL; register a disk you baked or obtained yourself.

| Field | Value |
|-------|--------|
| ID | `grain-ubuntu` |
| LocalOnly | yes (`grain image pull` refuses; use import) |
| HasAgent | true (create prefers agent wait before SSH deploy) |
| SSH user | `ubuntu` |

### Import a baked disk

```bash
grain image import ./golden.qcow2
grain image import ./golden.qcow2 --id grain-ubuntu
grain image ls
grain new -i grain-ubuntu
```

Import copies/converts the source into `~/.grain/images/grain-ubuntu/disk.qcow2`, writes `has_agent=true` and `ssh_user`, and flattens qcow2 overlay chains when `qemu-img` is available.

### Bake from ubuntu-cloud (automated)

On a Mac with QEMU:

```bash
make build agent-linux
brew install qemu
./scripts/bake-golden.sh
# or dry-run:
./scripts/bake-golden.sh --dry-run
```

The script:

1. Ensures `grain-agent-linux-*` and `ubuntu-cloud`
2. Creates a persistent bake VM (SSH deploy of the agent after boot)
3. Enables `grain-agent` for future boots
4. Stops the VM cleanly
5. `qemu-img convert -O qcow2` flattens the overlay into a standalone base
6. `grain image import … --id grain-ubuntu`

Env knobs: `BAKE_VM`, `IMAGE_ID`, `GRAIN_BIN`, `GRAIN_DATA_DIR`, `KEEP_BAKE_VM=1`.

### Bake manually

```bash
make agent-linux
grain up
grain image pull ubuntu-cloud
grain new -p -n bake-vm -i ubuntu-cloud
grain x bake-vm -- sudo systemctl enable grain-agent
grain stop bake-vm
qemu-img convert -O qcow2 ~/.grain/vms/bake-vm/disk.img.qcow2 /tmp/grain-ubuntu.qcow2
grain image import /tmp/grain-ubuntu.qcow2 --id grain-ubuntu
grain rm bake-vm
grain new -i grain-ubuntu
```

### Why bake?

| Path | First create | Agent on boot |
|------|--------------|---------------|
| `ubuntu-cloud` | pull once + SSH deploy agent each new guest (or reuse if already on disk) | after deploy |
| `grain-ubuntu` | local base only; agent already in image | systemd enable from bake |

Config can stay `image: ubuntu-cloud`. Prefer the golden id when local:

```bash
# optional helper in tooling: image.DefaultIDFor(dataDir) returns grain-ubuntu
# when that base is Ready, else ubuntu-cloud
grain new -i grain-ubuntu
```

## Download once

Images land under `~/.grain/images/<id>/` (e.g. `disk.qcow2`).  
`grain image pull` **no-ops** if a usable base disk is already present. Each new VM uses a qcow2 overlay (or CoW clone) on top of that shared base—you do not re-download per sandbox.

```bash
grain image pull    # once per machine (or after deleting the image dir)
grain new           # overlay on local base
grain new           # same base again
```

## SHA-256 verification

Catalog entries pin a **SHA-256** digest for the current noble minimal release files. After download (before rename into place):

1. grain hashes the partial file
2. on **mismatch**, the partial is deleted and pull fails with `sha256 mismatch: got … want …`
3. on success, the file is renamed to `disk.qcow2` (or `.img`) and `source.url` / `ssh_user` hints are written

If a digest is empty in the catalog (dev-only), verification is skipped.

When Ubuntu rolls the release pointer, digests in the catalog must be refreshed to match [SHA256SUMS](https://cloud-images.ubuntu.com/minimal/releases/noble/release/SHA256SUMS).

## Why not tiny custom images by default?

grain targets **real cloud-init Linux** sandboxes: SSH, packages, agents, k3s labs. That needs:

| Requirement | Cloud image | Tiny custom |
|-------------|-------------|-------------|
| cloud-init NoCloud seed (SSH keys, hostname, runcmd, mounts) | yes | often missing or half-broken |
| virtio disk/net + UEFI (esp. aarch64) | tested | easy to misconfigure |
| familiar `apt` / `ubuntu` user | yes | custom users and paths |
| security updates from upstream | yes | you own the rebuild |

A smaller rootfs can be added later as another catalog id; the default stays a known-good Ubuntu cloud image so `grain new` + `grain sh` works without hand-rolled kernels or init. Golden images (`grain-ubuntu`) layer agent readiness on that same base via local import.

## Related

- [Troubleshooting](troubleshooting.md) — pull failures, doctor image check
- [Mounts](mounts.md) — 9p shares into the guest
