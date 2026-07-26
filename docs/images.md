# Base images

## Default image selection (`auto`)

Config default is `image: auto`:

1. If a local **`grain-ubuntu`** golden disk is Ready under `~/.grain/images/`, use it.
2. Otherwise use **`ubuntu-cloud`** (downloadable Ubuntu 24.04 minimal cloud).

```bash
grain image ls
grain image pull ubuntu-cloud   # pull catalog cloud image
grain new                       # auto → grain-ubuntu if imported, else ubuntu-cloud
grain new -i ubuntu-cloud       # force cloud image
```

Config:

```yaml
image: auto          # prefer golden when present (default)
# image: ubuntu-cloud
# image: grain-ubuntu
ssh_user: ubuntu
```

**Wait mode:** when the selected image has a baked agent (`HasAgent` / `has_agent` meta), create defaults to `--wait agent`. Otherwise `--wait ssh` (soft agent deploy). Override with `grain new --wait ssh|agent|userdata`.

## Catalog: `ubuntu-cloud`

- arm64 / amd64 from [Ubuntu cloud images](https://cloud-images.ubuntu.com/minimal/releases/noble/release/)
- qcow2, **cloud-init** NoCloud, SSH user **`ubuntu`**
- ~300 MB download
- **HasAgent: false** — grain deploys `grain-agent` over SSH when `~/.grain/agent/grain-agent-linux-*` or `make agent-linux` is available

## Golden image: `grain-ubuntu`

`grain-ubuntu` is currently **local-only**: Ubuntu + **grain-agent baked in**. Register a disk you baked; public pull URL may land in a later release.

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

### Minimal cloud-init seed for golden clones

When the base image is agent-ready (`HasAgent` / local `has_agent` meta), create (and Start when regenerating a missing seed) writes a **minimal** NoCloud user-data instead of the full first-boot document:

| Full seed (`ubuntu-cloud`) | Minimal seed (`grain-ubuntu` / HasAgent) |
|----------------------------|------------------------------------------|
| Hostname, keys, grain user | Same |
| Standard cloud-init module set | Limited `cloud_config_modules` (hostname, hosts, users, ssh, runcmd) |
| Default package behaviour | `package_update` / `package_upgrade` false |
| runcmd: SSH inject + grain-ready | Single runcmd: SSH inject + `userdata-ran` + grain-ready |

The seed ISO is still attached for per-clone **hostname**, **SSH keys**, and **9p mount** runcmds. Heavy package installs and long cloud-init stages are avoided because the golden already has the agent and base tooling.

Bake prepares the disk for this path (`cloud-init clean`, `userdata-ran` stamp, enabled `grain-agent`, cleared `machine-id`). Clones are expected to finish cloud-init and report agent-ready sooner than a cold `ubuntu-cloud` first boot; measure locally if you need hard p50 numbers.

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
3. Enables `grain-agent`, runs `cloud-init clean --logs`, stamps `/var/lib/grain/userdata-ran`, and clears `/etc/machine-id` (systemd regenerates a unique id per clone)
4. Stops the VM cleanly
5. `qemu-img convert -O qcow2` flattens the overlay into a standalone base
6. `grain image import … --id grain-ubuntu`

Env knobs: `BAKE_VM`, `IMAGE_ID`, `GRAIN_BIN`, `GRAIN_DATA_DIR`, `KEEP_BAKE_VM=1`, `ARTIFACT_DIR` (with `--ci`).

### CI bake artifacts

GitHub Actions workflow [`.github/workflows/bake-golden.yml`](../.github/workflows/bake-golden.yml) builds `grain-ubuntu` on a schedule (weekly) and on manual **workflow_dispatch**.

| Output | Notes |
|--------|--------|
| `grain-ubuntu-amd64.qcow2` | Flattened golden disk (ubuntu-cloud + grain-agent, `has_agent`) |
| `grain-ubuntu-amd64.qcow2.sha256` | SHA-256 checksum file |

**Arch:** `ubuntu-latest` is **amd64** only. arm64 golden bakes need a **self-hosted** runner with QEMU/KVM (not wired into the matrix yet).

**KVM:** grain QEMU uses `-cpu host`, which needs KVM on Linux (or HVF on macOS). Many GitHub-hosted runners lack `/dev/kvm`; the job may fail or time out under pure TCG. Prefer a self-hosted runner with KVM, or bake on a laptop and use the artifact/import path below.

#### Download from Actions

1. Open the repo on GitHub → **Actions** → workflow **Bake golden image**.
2. Open a successful run → **Artifacts** → download `grain-ubuntu-amd64`.
3. Unpack if needed, then import:

```bash
grain image import ./grain-ubuntu-amd64.qcow2 --id grain-ubuntu
# optional: verify checksum
sha256sum -c grain-ubuntu-amd64.qcow2.sha256
grain image ls
grain new -i grain-ubuntu
```

#### Local / CI script

```bash
make build agent-linux
# Full CI path (isolated data dir, writes dist/golden/…):
./scripts/ci-bake-golden.sh
# or:
./scripts/bake-golden.sh --ci
# ARTIFACT_DIR=./out CI_READY_TIMEOUT=15m ./scripts/bake-golden.sh --ci
```

Optional **workflow_dispatch** input `release_tag` (e.g. `v0.1.0`) attaches the qcow2 + checksum to an **existing** GitHub Release when `GITHUB_TOKEN` can write contents.

The catalog entry for `grain-ubuntu` stays **`LocalOnly: true`** until a real public download URL is published (no placeholder URLs). See the comment in `internal/image/catalog.go`.

### Bake manually

```bash
make agent-linux
grain up
grain image pull ubuntu-cloud
grain new -p -n bake-vm -i ubuntu-cloud
grain x bake-vm -- sudo systemctl enable grain-agent
grain x bake-vm -- sudo cloud-init clean --logs
grain x bake-vm -- sudo mkdir -p /var/lib/grain && sudo touch /var/lib/grain/userdata-ran
grain x bake-vm -- sudo truncate -s 0 /etc/machine-id   # unique id regenerated on clone boot
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

## Firecracker rootfs (experimental)

When `hypervisor: firecracker`, guests need a **raw** root disk and a separate **vmlinux** kernel. Catalog qcow2 cloud images are converted with `qemu-img convert -O raw` at Start when possible; otherwise Start asks for a raw golden.

Firecracker does not use UEFI the same way as QEMU aarch64 cloud boots. Prefer a FC-oriented kernel + rootfs pair. See [firecracker.md](firecracker.md) for layout, vsock agent, and limits.

## Related

- [Troubleshooting](troubleshooting.md) — pull failures, doctor image check
- [Firecracker](firecracker.md) — experimental FC backend, kernel/rootfs, vsock
- [Mounts](mounts.md) — 9p shares into the guest
