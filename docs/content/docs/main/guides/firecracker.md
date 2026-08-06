---
title: "Firecracker on Linux"
description: "Firecracker backend on Linux+KVM: agent production (vFC-1) over vsock, catalog pull, doctor; partial TAP publish/fwd (vFC-2)."
section: guides
keywords:
  - Firecracker
  - KVM
  - hypervisor
  - Linux
  - vsock
  - rootfs
  - kernel_path
  - vFC-1
  - support policy
---

{{< only-need href="get-started/quickstart/" >}}
Default backend is QEMU — use that path unless you deliberately need Firecracker.
{{< /only-need >}}

grain can launch sandboxes with [Firecracker](https://firecracker-microvm.github.io/) instead of QEMU on **Linux + KVM**.

## Support policy

| Tier | Status | What works |
|------|--------|------------|
| **FC agent production (vFC-1)** | **Supported** | Pull `fc-kernel` + `grain-ubuntu-fc`; doctor; `grain new --wait agent`; `grain x` / agent shell / cp / sync / MCP guest tools over vsock UDS + `CONNECT` |
| **FC net (vFC-2 partial)** | **Supported** | TAP + create-time `-P` / `--publish` (DNAT + SNAT to TAP HostIP so loopback clients work), `grain fwd add/ls/rm` (TCP proxy to guest IP), optional SSH/agent host ports. Requires **CAP_NET_ADMIN** and `/dev/net/tun`. |
| **Still QEMU-only** | — | Overlay L2, 9p/virtiofs mounts, SLIRP `10.0.2.2` proxy, virtio GPU |
| **Default product path** | **QEMU** | macOS + Linux; full SLIRP/publish/mounts/overlay/GPU where applicable |

Default remains `hypervisor: qemu`. Jailer-less FC launch (single-tenant only). Nested KVM: works when the outer hypervisor exposes `vmx`/`svm` so `/dev/kvm` exists in this guest.

macOS, missing Firecracker binary, or unusable **`/dev/kvm`** fail with clear errors (`grain doctor` and create). Networking failures without CAP_NET_ADMIN mention privilege in the error.

## When to use this path

| Use Firecracker when… | Prefer QEMU when… |
|------------------------|-------------------|
| You are on **Linux with KVM** and want agent-first microVMs | You need macOS host or overlay/mounts/GPU |
| Catalog **`grain-ubuntu-fc` / `fc-kernel`** (or BYO raw + vmlinux) is enough | You need full QEMU cloud-image + SLIRP UX |
| You want publish/fwd on Linux without QEMU | You need multi-VM overlay L2 |

## Quick config

```yaml
# ~/.grain/config.yaml
hypervisor: firecracker
firecracker_binary: firecracker   # PATH lookup (default)
kernel_path: ""                   # optional; default ~/.grain/kernels/vmlinux
```

| Key | Default | Meaning |
|-----|---------|---------|
| `hypervisor` | `qemu` | Set to `firecracker` to select this backend (daemon restart after change) |
| `firecracker_binary` | `firecracker` | Absolute path or name on `PATH` |
| `kernel_path` | empty | Guest **vmlinux**; empty → `~/.grain/kernels/vmlinux` (under `data_dir`) |

See [Configuration](../../reference/config/#firecracker-experimental).

## Requirements

| Dependency | Notes |
|------------|--------|
| **Linux** | Firecracker is Linux-only (`KVM`). On macOS: `firecracker requires linux`. |
| **firecracker** binary | On `PATH`, or set `firecracker_binary` |
| **Guest kernel** (`vmlinux`) | Uncompressed Linux kernel built for Firecracker (virtio MMIO, no PCI). Default path: `~/.grain/kernels/vmlinux` or `kernel_path` |
| **Raw rootfs** | Firecracker root drives are **raw** block files, not qcow2 |
| **qemu-img** | Used to convert qcow2 → raw when the VM disk is a qcow2 overlay (`grain doctor` flags if missing) |
| **KVM** | `/dev/kvm` accessible to the grain daemon user (**required** — no TCG fallback) |
| **Nested virt** | If grain runs *inside* a VM, the outer hypervisor must expose `vmx` (Intel) or `svm` (AMD) so `/dev/kvm` exists in the guest |

### Operator checklist (vFC-1 production path)

1. Linux host with `/dev/kvm` RDWR for the daemon user (add user to `kvm` group if needed).
2. Install Firecracker and put it on `PATH` (or set `firecracker_binary`).
3. **Pull** catalog artifacts: `grain image pull fc-kernel` and `grain image pull grain-ubuntu-fc` (or BYO: place `vmlinux` / `grain image import …`).
4. Set `hypervisor: firecracker` and preferably `image: grain-ubuntu-fc` in `~/.grain/config.yaml`, then `grain up` (restart daemon if it was already running).
5. Run `grain doctor` and fix every `✗` before `grain new -i grain-ubuntu-fc --wait agent`.
6. Optional BYO: raw rootfs via `grain image import ./rootfs.ext4 --id my-fc-rootfs` (not catalog qcow2 cloud images).

## `grain doctor` (Firecracker)

With `hypervisor: firecracker` in config:

```bash
grain doctor
```

| Check | Severity | What it means |
|-------|----------|----------------|
| `firecracker` (or `firecracker_binary`) | **Hard** | Binary missing, or not Linux |
| `/dev/kvm` | **Hard** | Missing or not RDWR — Firecracker cannot start |
| Nested virt CPU flags | Soft (`·`) | Host looks like a VM without `vmx`/`svm` |
| Firecracker kernel | **Hard** | Missing default `…/kernels/vmlinux`, or **BYO misconfigured** when `kernel_path` is set but empty/absent. Fix: `grain image pull fc-kernel` (or place vmlinux / `grain image import <vmlinux> --id fc-kernel`) |
| `qemu-img` | **Hard** | Needed to convert qcow2 disks to raw at Start |
| QEMU system binary | Soft | Optional when hypervisor is firecracker |
| Base image | **Hard** | Default image not ready — for `grain-ubuntu-fc`: `grain image pull grain-ubuntu-fc` (import is BYO fallback) |
| FC catalog rootfs / QEMU default | Soft (`·`) | Notes when default image is QEMU-oriented or `grain-ubuntu-fc` not pulled |
| Agent binary / socket | Soft | Optional agent host binary; daemon up |

Hard failures print `✗` and exit non-zero. Soft items print `·` and do not fail doctor.

Doctor **distinguishes** “no kernel at the default path” (missing Grain/BYO artifact) from “you set `kernel_path` and that file is gone” (BYO misconfigured).

If `grain new` fails, prefer the **create error** and `~/.grain/logs/<name>.log` over later agent/vsock messages — Firecracker often exits immediately when KVM is unavailable (`firecracker exited immediately` + KVM hint).

## Image / rootfs notes

grain’s QEMU catalog images (`ubuntu-cloud`, `grain-ubuntu`, `alpine-cloud`) are **qcow2 cloud images** aimed at QEMU + cloud-init. They are **not** drop-in Firecracker guests.

### Firecracker catalog IDs (vFC-1 production)

| Catalog ID | Role | Status today |
|------------|------|----------------|
| **`grain-ubuntu-fc`** | Raw rootfs with **grain-agent** baked in (`format: raw`, `HasAgent`) | **Pullable** from `fc-latest` → `images/grain-ubuntu-fc/disk.raw` (or BYO `import`) |
| **`fc-kernel`** | Guest **vmlinux** artifact | **Pullable** from `fc-latest` → `~/.grain/kernels/vmlinux` (or BYO `import` / `kernel_path`) |

These IDs are **explicit** (not dual-use of `grain-ubuntu` qcow2) so operators and tooling never confuse QEMU cloud images with FC raw + kernel. **Pull is the happy path**; BYO import remains first-class.

### Pull (published `fc-latest`)

```bash
# config: hypervisor: firecracker
grain image pull fc-kernel          # → data_dir/kernels/vmlinux
grain image pull grain-ubuntu-fc    # → images/grain-ubuntu-fc/disk.raw
grain new -i grain-ubuntu-fc --wait agent
# or: ./scripts/smoke-fc.sh
```

Catalog digests use companion `.sha256` sidecars on the `fc-latest` release (fail-closed; same pattern as `grain-ubuntu`).

### Bake (Linux rebuild)

```bash
# curl, qemu-img, unsquashfs, mkfs.ext4, go
./scripts/bake-fc.sh --all
# → dist/fc/vmlinux-<arch> + grain-ubuntu-fc-<arch>.raw (+ .sha256)

grain image import dist/fc/vmlinux-amd64 --id fc-kernel
grain image import dist/fc/grain-ubuntu-fc-amd64.raw --id grain-ubuntu-fc
```

Defaults: Firecracker CI `v1.12` `vmlinux-6.1.128` + `ubuntu-24.04.squashfs` → ext4 with agent systemd unit (vsock :7475). Override with `FC_CI_VERSION` / `FC_KERNEL_VER` / `FC_UBUNTU_SQFS`. Workflow **Bake Firecracker artifacts** rewrites the `fc-latest` release.

### Operator path today (BYO)

1. Prefer a **raw** golden rootfs (ext4/squashfs layout that boots with the FC kernel’s `root=/dev/vda`).
2. If the VM disk is still **qcow2**, Start runs `qemu-img convert -O raw` into `disk.raw` under the VM dir (when `qemu-img` is available). Otherwise Start refuses with a conversion hint.
3. Standard Ubuntu cloud images need a **matching Firecracker-capable kernel**; they are not drop-in FC guests without extra work (kernel + init + virtio drivers).

```bash
# BYO kernel → catalog id (installs under data_dir/kernels/vmlinux)
grain image import ./vmlinux --id fc-kernel

# BYO raw rootfs → catalog id (images/grain-ubuntu-fc/disk.raw; keeps format raw)
grain image import ./rootfs.ext4 --id grain-ubuntu-fc

# Create with the FC rootfs id (hypervisor: firecracker in config)
grain new -i grain-ubuntu-fc --wait agent
```

See also [Images](../images/#firecracker-rootfs-experimental) for the QEMU/golden workflow. FC agent production (vFC-1) is a separate catalog/kernel path from QEMU cloud images.

### Suggested layout

```text
~/.grain/
  kernels/
    vmlinux              # Firecracker guest kernel
  config.yaml            # hypervisor: firecracker
  vms/<name>/
    disk.raw             # rootfs (converted or imported)
    firecracker.json     # generated config
    firecracker.sock     # FC API unix socket
    fc-vsock.sock        # host end of virtio-vsock
    firecracker.pid
```

## Networking and agent

This backend is **CNI-less / jailer-less**. Agent uses vsock; optional TAP + DNAT/SNAT provides publish/fwd (not SLIRP).

| Channel | Status |
|---------|--------|
| SSH / port forwards (`-P`, `grain fwd`) | **Supported (vFC-2)** — TAP + DNAT/SNAT / TCP proxy (needs CAP_NET_ADMIN) |
| Overlay / shared L2 | **Not available** (use QEMU) |
| grain-agent | **Supported** — Firecracker vsock UDS + `CONNECT` (vFC-1); optional TCP DNAT |

### Publish example (Linux + CAP_NET_ADMIN)

```bash
# config: hypervisor: firecracker
grain image pull fc-kernel
grain image pull grain-ubuntu-fc
grain up
grain new -i grain-ubuntu-fc -n fcweb -P 18080:80 --wait agent
# Guest eth0 is configured via agent after boot; then:
curl -sS http://127.0.0.1:18080/   # if guest serves :80
grain fwd add fcweb 19000:7475     # live TCP proxy to guest
grain fwd ls fcweb
```

Smoke: `./scripts/smoke-fc-net.sh` (guest HTTP listener + host `curl` for create-time `-P` and live `fwd`).

On Start, grain:

- Allocates a guest **CID** (`AgentCID`) and configures Firecracker `vsock` with `uds_path` = `…/fc-vsock.sock`
- When net is enabled: creates TAP, allocates SSH/agent host ports, applies DNAT for `-P` publishes **and SNAT** so DNATed loopback clients appear as the TAP HostIP; after agent is up, configures guest eth0 via agent exec

Firecracker’s host-side vsock is **not** AF_VSOCK/`/dev/vhost-vsock`. Host clients connect to the UDS and send `CONNECT <port>\n` (see [Firecracker vsock docs](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)). Guest agent listens on AF_VSOCK port **7475**. Host `agent.Dial` prefers UDS + CONNECT (vFC-1).

For the QEMU networking model (SLIRP, publish, live forwards), see [Networking](../networking/).

## Start / stop / pause

| Operation | Behavior |
|-----------|----------|
| **Start** | Writes `firecracker.json`, runs jailer-less `firecracker --api-sock … --config-file …` |
| **Stop** | `SendCtrlAltDel` via FC API, then SIGTERM/SIGKILL |
| **Pause / Resume** | `PATCH /vm` with `Paused` / `Resumed` when the API socket is up |
| **SaveVM / suspend snapshot** | Unsupported (`savevm is not supported for firecracker`) |

Logs: `~/.grain/logs/<name>.log` (Firecracker stdout/stderr). `grain logs --qemu <name>` shows that hypervisor log (name is historical).

## Cloud-init seed

If `seed.iso` exists in the VM dir, it is attached as a second read-only drive (`cidata`). NoCloud typically expects a labeled ISO/FAT volume; success depends on the guest image. Prefer baking keys/agent into a FC-oriented rootfs for reliable boots.

## Example

```yaml
# ~/.grain/config.yaml
hypervisor: firecracker
firecracker_binary: firecracker
image: grain-ubuntu-fc
cpus: 2
memory_mb: 1024
```

```bash
# vFC-1 production path (published fc-latest)
grain image pull fc-kernel
grain image pull grain-ubuntu-fc
grain up
grain doctor
grain new -i grain-ubuntu-fc --wait agent

# BYO alternative
# grain image import ./rootfs.ext4 --id my-fc-rootfs
# grain new -i my-fc-rootfs --wait agent
```

## Known limitations vs QEMU

| Capability | QEMU (default) | Firecracker (vFC-1 agent production) |
|------------|----------------|--------------------------------------|
| Host OS | macOS + Linux | **Linux only** |
| Acceleration | HVF / KVM (TCG fallback on Linux) | **KVM required** (no TCG) |
| Catalog images | QEMU cloud qcow2 first-class | **`fc-kernel` + `grain-ubuntu-fc` pullable** (`fc-latest`); QEMU cloud images not drop-in |
| Guest kernel | QEMU/UEFI path | Catalog **`fc-kernel`** → `vmlinux` (or `kernel_path` / BYO import) |
| SSH + hostfwd / `-P` | Yes (SLIRP) | **Yes** — TAP DNAT / TCP proxy (CAP_NET_ADMIN) |
| Guest agent reachability | TCP hostfwd and/or vhost-vsock | **Supported** — FC vsock UDS + `CONNECT` (primary); optional TCP DNAT |
| 9p / virtiofs mounts | Yes | **No** (not wired; vFC-2) |
| Overlay network | Yes | **No** (vFC-2) |
| Egress proxy via SLIRP | Yes | **No** host path (vFC-2) |
| GPU (`virtio`) | Yes | **No** |
| Suspend / savevm | Yes | **Unsupported** |
| Pause / resume | QMP | FC API (when socket up) |
| Jailer / production isolation extras | N/A | **Jailer-less** (optional later; not multi-tenant) |
| `agent_transport` config | auto / tcp / vsock | Ignored (FC vsock always) |

**Not on FC today (use QEMU):** multi-host CNI, overlay L2, 9p/virtiofs mounts, SLIRP proxy, virtio GPU. Jailer multi-tenant claims are out of scope.

**vFC-1 (agent) + vFC-2 (partial net) shipped:** pullable `fc-latest`, vsock agent, TAP publish/fwd. Full table: [Hypervisor matrix](../../explain/hypervisor-matrix/).

### Boot metric (reference SKU)

Primary project metric: wall time for `grain new -i grain-ubuntu-fc --wait agent` (create through agent ready).

| Field | Value |
|-------|--------|
| **Reference host class** | **AWS `m7i-flex.large` nested-virt x86_64** (Ubuntu 24.04 guest host, `/dev/kvm`, Firecracker on PATH) |
| **How to measure** | `./scripts/bench-fc.sh -n 5` (wraps `bench-create.sh` with `grain-ubuntu-fc` + `--wait agent`) |
| **Smoke** | `./scripts/smoke-fc.sh` |
| **Sample (2026-08, post-merge main)** | **p50 ≈ 1986 ms**, **p95 ≈ 2166 ms** (N=5 create→agent ready on this SKU) |

Nested virt is slower than bare-metal KVM; re-run `bench-fc.sh` on your class before publishing numbers in a release.

## Related

- [Hypervisor matrix](../../explain/hypervisor-matrix/) — QEMU vs FC today + vFC-1 / vFC-2 targets
- [Images](../images/#firecracker-rootfs-experimental) — base images, golden bake (QEMU-oriented); FC rootfs notes
- [Networking](../networking/) — QEMU SLIRP / hostfwd (not used by FC)
- [Guest agent](../agent/) — guest agent HTTP API
- [Troubleshooting](../troubleshooting/) — doctor and logs (includes Firecracker doctor rows)
- [Configuration](../../reference/config/#firecracker-experimental) — `hypervisor`, `firecracker_binary`, `kernel_path`
- [Concepts](../../get-started/concepts/#hypervisors) — hypervisor glossary
- [Product surface](../../explain/parity/) — FC agent production vs QEMU-only net
