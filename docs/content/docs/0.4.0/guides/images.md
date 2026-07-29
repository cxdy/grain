---
title: "Images and golden boots"
description: "Pull, import, bake, and choose base images."
section: guides
---


## Default image selection (`auto`)

Config default is `image: auto`:

1. If a local **`grain-ubuntu`** golden disk is Ready under `~/.grain/images/`, use it.
2. Otherwise use **`ubuntu-cloud`** (downloadable Ubuntu 24.04 minimal cloud).

```bash
grain image ls
grain image pull grain-ubuntu     # golden (agent baked in) from GitHub Releases
grain image pull ubuntu-cloud     # Ubuntu 24.04 minimal cloud
grain image pull alpine-cloud     # Alpine Linux cloud (multi-distro)
grain new                         # auto → grain-ubuntu if local, else ubuntu-cloud
grain new -i ubuntu-cloud         # force Ubuntu cloud image
grain new -i alpine-cloud         # Alpine cloud (SSH user alpine)
```

Config:

```yaml
image: auto          # prefer golden when present (default)
# image: ubuntu-cloud
# image: grain-ubuntu
# image: alpine-cloud
ssh_user: ubuntu     # override per image when needed (alpine-cloud → alpine)
```

**Wait mode:** empty/`auto` (the create default) picks **`agent`** when the selected image has a baked agent (`HasAgent` / `has_agent` meta), otherwise **`ssh`** (soft agent deploy). Override with `grain new --wait ssh|agent|userdata`.

## Catalog: `ubuntu-cloud`

- arm64 / amd64 from [Ubuntu cloud images](https://cloud-images.ubuntu.com/minimal/releases/noble/release/)
- qcow2, **cloud-init** NoCloud, SSH user **`ubuntu`**
- ~300 MB download
- **HasAgent: false** — grain deploys `grain-agent` over SSH when `~/.grain/agent/grain-agent-linux-*` or `just agent-linux` is available

## Catalog: `alpine-cloud`

- arm64 / amd64 from [Alpine cloud images](https://alpinelinux.org/cloud/) (`generic` UEFI + cloud-init qcow2)
- qcow2, **cloud-init** (NoCloud auto-detected on the generic image), SSH user **`alpine`**
- ~200–240 MB download (no pinned SHA256 — Alpine publishes GPG `.asc`, not sha256sum sidecars)
- **HasAgent: false** — same SSH agent deploy path as `ubuntu-cloud`
- Filenames use Alpine arch names (`aarch64` / `x86_64`); grain maps `GOARCH` accordingly

```bash
grain image pull alpine-cloud
grain new -i alpine-cloud
# cloud-init still injects SSH keys for user "alpine"
```

Refresh the catalog version when Alpine rolls a new cloud release under `dl-cdn.alpinelinux.org/alpine/v*/releases/cloud/`.

## Golden image: `grain-ubuntu`

Ubuntu + **grain-agent baked in**. Prefer pull when online; import still works fully offline.

| Field | Value |
|-------|--------|
| ID | `grain-ubuntu` |
| LocalOnly | no (pullable on amd64/arm64) |
| HasAgent | true (create prefers agent wait before SSH deploy) |
| SSH user | `ubuntu` |
| URL | `https://github.com/cxdy/grain/releases/download/golden-latest/grain-ubuntu-{arch}.qcow2` |

### Pull from GitHub Releases (`golden-latest`)

The bake workflow publishes a movable release tag **`golden-latest`** (not a code `v*` release) and rewrites its assets on every successful bake:

| Asset | Notes |
|-------|--------|
| `grain-ubuntu-amd64.qcow2` | x86_64 golden (CI on `ubuntu-latest`) |
| `grain-ubuntu-amd64.qcow2.sha256` | companion checksum (sha256sum format) |
| `grain-ubuntu-arm64.qcow2` | aarch64 — needs self-hosted KVM runner (not matrixed yet) |

```bash
grain image pull grain-ubuntu
grain image ls                  # LOCAL=yes for grain-ubuntu
grain new -i grain-ubuntu
```

Pull downloads the large qcow2 with progress, then verifies against the companion `.sha256` sidecar when present (catalog SHA256 is empty so the sidecar is authoritative). If the sidecar is missing, verification is skipped.

Import remains the offline / custom path:

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
just build && just agent-linux
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

#### Prefer pull (after a successful bake published `golden-latest`)

```bash
grain image pull grain-ubuntu
grain new -i grain-ubuntu
```

#### Download from Actions (fallback / pre-release)

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
just build && just agent-linux
# Full CI path (isolated data dir, writes dist/golden/…):
./scripts/ci-bake-golden.sh
# or:
./scripts/bake-golden.sh --ci
# ARTIFACT_DIR=./out CI_READY_TIMEOUT=15m ./scripts/bake-golden.sh --ci
```

Every successful bake **always** updates the `golden-latest` release (create tag if missing, replace assets). Optional **workflow_dispatch** input `release_tag` (e.g. `v0.2.0`) *also* attaches the qcow2 + checksum to that existing code release. Code releases (`release.yml` on `v*`) stay binary-only — no golden coupling.

### Bake manually

```bash
just agent-linux
grain up
grain image pull ubuntu-cloud
grain new -p -n bake-vm -i ubuntu-cloud
# Agent runs as root — use one remote sh -c so && does not run on the host.
grain x bake-vm -- sh -c 'systemctl enable grain-agent'
grain x bake-vm -- sh -c 'cloud-init clean --logs'
grain x bake-vm -- sh -c 'mkdir -p /var/lib/grain && touch /var/lib/grain/userdata-ran'
grain x bake-vm -- sh -c 'truncate -s 0 /etc/machine-id'   # unique id regenerated on clone boot
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
| `grain-ubuntu` | pull once (or import) + agent already in image | systemd enable from bake |

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

## Boot latency bench

Measure create readiness (image must already be local; daemon up):

```bash
grain up
grain image pull grain-ubuntu   # or ubuntu-cloud / alpine-cloud
./scripts/bench-create.sh                 # default N=5, auto wait
./scripts/bench-create.sh -n 10 -i grain-ubuntu --wait agent
N=10 IMAGE=ubuntu-cloud WAIT=ssh ./scripts/bench-create.sh
```

The script times N `grain new` runs, deletes each VM after timing (unless `--keep`), and prints **min / max / avg / p50 / p95** in milliseconds. Use it to compare cold `ubuntu-cloud` (SSH + agent deploy) vs golden `grain-ubuntu` (agent already in image).

## SHA-256 verification

Catalog entries pin a **SHA-256** digest for the current noble minimal release files. After download (before rename into place):

1. grain hashes the partial file
2. on **mismatch**, the partial is deleted and pull fails with `sha256 mismatch: got … want …`
3. on success, the file is renamed to `disk.qcow2` (or `.img`) and `source.url` / `ssh_user` hints are written

If a digest is empty in the catalog, pull tries a companion **`URL.sha256`** sidecar (sha256sum format). When neither pin nor sidecar is available, verification is skipped.

When Ubuntu rolls the release pointer, digests in the catalog must be refreshed to match [SHA256SUMS](https://cloud-images.ubuntu.com/minimal/releases/noble/release/SHA256SUMS).

## Why not tiny custom images by default?

grain targets **real cloud-init Linux** sandboxes: SSH, packages, agents, k3s labs. That needs:

| Requirement | Cloud image | Tiny custom |
|-------------|-------------|-------------|
| cloud-init NoCloud seed (SSH keys, hostname, runcmd, mounts) | yes | often missing or half-broken |
| virtio disk/net + UEFI (esp. aarch64) | tested | easy to misconfigure |
| familiar `apt` / `ubuntu` user | yes | custom users and paths |
| security updates from upstream | yes | you own the rebuild |

A smaller rootfs can be added later as another catalog id; the default stays a known-good Ubuntu cloud image so `grain new` + `grain sh` works without hand-rolled kernels or init. Golden images (`grain-ubuntu`) layer agent readiness on that same base via `grain image pull` or local import.

## Firecracker rootfs (experimental)

When `hypervisor: firecracker`, guests need a **raw** root disk and a separate **vmlinux** kernel. Catalog qcow2 cloud images are converted with `qemu-img convert -O raw` at Start when possible; otherwise Start asks for a raw golden.

Firecracker does not use UEFI the same way as QEMU aarch64 cloud boots. Prefer a FC-oriented kernel + rootfs pair. See [firecracker.md](/docs/0.4.0/guides/firecracker/) for layout, vsock agent, and limits.

## Related

- [Troubleshooting](/docs/0.4.0/guides/troubleshooting/) — pull failures, doctor image check
- [Firecracker](/docs/0.4.0/guides/firecracker/) — experimental FC backend, kernel/rootfs, vsock
- [Agent](/docs/0.4.0/guides/agent/) — wait modes, deploy, golden HasAgent path
- [Mounts](/docs/0.4.0/guides/mounts/) — 9p shares into the guest
