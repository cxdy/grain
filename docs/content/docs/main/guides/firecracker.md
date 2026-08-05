---
title: "Firecracker on Linux (experimental)"
description: "Experimental Firecracker hypervisor path: Linux+KVM, raw rootfs, vsock agent, doctor checks, and limits vs QEMU."
section: guides
keywords:
  - Firecracker
  - KVM
  - hypervisor
  - Linux
  - experimental
  - vsock
  - rootfs
  - kernel_path
---

{{< only-need href="get-started/quickstart/" >}}
Default backend is QEMU — use that path unless you deliberately need Firecracker.
{{< /only-need >}}

grain can launch sandboxes with [Firecracker](https://firecracker-microvm.github.io/) instead of QEMU.

**Status: experimental.** This page is the supported **operator path** for trying Firecracker today. It is **not** a production-hardened backend: no SLIRP/hostfwd networking, no jailer, limited image story, and several QEMU features are missing. Default remains `hypervisor: qemu`. The mock backend is unchanged for unit tests.

macOS, hosts without the Firecracker binary, and hosts without a usable **`/dev/kvm`** fail with clear errors (`grain doctor` and create both surface KVM issues).

## When to use this path

| Use Firecracker (experimental) when… | Prefer QEMU when… |
|--------------------------------------|-------------------|
| You are on **Linux with KVM** and want a microVM backend | You need the default product path (macOS or Linux) |
| You bring your own **FC kernel + raw rootfs** | You want catalog images (`grain-ubuntu`, `ubuntu-cloud`) with SSH |
| You accept **vsock-only** agent access (no hostfwd) | You need publish ports, SSH, overlay, mounts, proxy, GPU |

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

### Operator checklist

1. Linux host with `/dev/kvm` RDWR for the daemon user (add user to `kvm` group if needed).
2. Install Firecracker and put it on `PATH` (or set `firecracker_binary`).
3. Place a Firecracker-capable `vmlinux` at `~/.grain/kernels/vmlinux` or set `kernel_path`.
4. Prefer a **raw** rootfs image (`grain image import ./rootfs.ext4 --id my-fc-rootfs`), not catalog qcow2 cloud images.
5. Set `hypervisor: firecracker` in `~/.grain/config.yaml`, then `grain up` (restart daemon if it was already running).
6. Run `grain doctor` and fix every `✗` before `grain new`.

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
| Firecracker kernel | **Hard** | Missing default `…/kernels/vmlinux`, or **BYO misconfigured** when `kernel_path` is set but empty/absent. Fix: place vmlinux, or `grain image import <vmlinux> --id fc-kernel` |
| `qemu-img` | **Hard** | Needed to convert qcow2 disks to raw at Start |
| QEMU system binary | Soft | Optional when hypervisor is firecracker |
| Base image | **Hard** | Default image not ready (`pull` or `import` as appropriate) |
| FC catalog rootfs / QEMU default | Soft (`·`) | Notes when default image is QEMU-oriented or `grain-ubuntu-fc` not imported |
| Agent binary / socket | Soft | Optional agent host binary; daemon up |

Hard failures print `✗` and exit non-zero. Soft items print `·` and do not fail doctor.

Doctor **distinguishes** “no kernel at the default path” (missing Grain/BYO artifact) from “you set `kernel_path` and that file is gone” (BYO misconfigured).

If `grain new` fails, prefer the **create error** and `~/.grain/logs/<name>.log` over later agent/vsock messages — Firecracker often exits immediately when KVM is unavailable (`firecracker exited immediately` + KVM hint).

## Image / rootfs notes

grain’s QEMU catalog images (`ubuntu-cloud`, `grain-ubuntu`, `alpine-cloud`) are **qcow2 cloud images** aimed at QEMU + cloud-init. They are **not** drop-in Firecracker guests.

### Reserved Firecracker catalog IDs (Phase 1 scaffold)

| Catalog ID | Role | Status today |
|------------|------|----------------|
| **`grain-ubuntu-fc`** | Raw rootfs with **grain-agent** baked in (`format: raw`, `HasAgent`) | **LocalOnly** — not pullable yet; `grain image import <raw> --id grain-ubuntu-fc` stores `images/grain-ubuntu-fc/disk.raw` |
| **`fc-kernel`** | Guest **vmlinux** artifact | **LocalOnly** — `grain image import <vmlinux> --id fc-kernel` installs to `~/.grain/kernels/vmlinux` (or set `kernel_path`) |

These IDs are **explicit** (not dual-use of `grain-ubuntu` qcow2) so operators and tooling never confuse QEMU cloud images with FC raw + kernel. Until GitHub/CI bake publishes digests + URLs, `grain image pull grain-ubuntu-fc` / `fc-kernel` refuses (local-only). BYO remains first-class.

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

See also [Images](../images/#firecracker-rootfs-experimental) for the QEMU/golden workflow; FC is a separate experimental path.

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

This backend is **CNI-less / TAP-less**: no SLIRP, no hostfwd, no SSH port, no overlay network, no egress-proxy hostfwd path.

| Channel | Status |
|---------|--------|
| SSH / port forwards (`-P`, `grain fwd`) | Not configured (experimental) |
| Overlay / shared L2 | Not used |
| grain-agent | **Firecracker vsock** only |

On Start, grain:

- Sets `SSHPort` / `AgentPort` to **0** (no TCP hostfwd)
- Allocates a guest **CID** (`AgentCID`, same allocator as QEMU vsock)
- Configures Firecracker `vsock` with `uds_path` = `…/fc-vsock.sock`

Firecracker’s host-side vsock is **not** AF_VSOCK/`/dev/vhost-vsock`. Host clients connect to the UDS and send `CONNECT <port>\n` (see [Firecracker vsock docs](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)). Guest agent listens on AF_VSOCK port **7475**.

QEMU’s `agent_transport: auto|tcp|vsock` path (vhost-vsock / TCP hostfwd) does not apply here. Host `agent.Dial` prefers Firecracker UDS + CONNECT when the instance has no TCP agent port (see production track **vFC-1**). The guest agent binary is unchanged.

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
kernel_path: /var/lib/grain/kernels/vmlinux-5.10
image: my-fc-rootfs   # local raw import; not ubuntu-cloud by default
cpus: 2
memory_mb: 1024
```

```bash
# Import a raw rootfs as a local image id (example)
grain image import ./rootfs.ext4 --id my-fc-rootfs

grain up
grain doctor
grain new -i my-fc-rootfs
grain stop <name>
```

## Known limitations vs QEMU

| Capability | QEMU (default) | Firecracker (experimental) |
|------------|----------------|----------------------------|
| Host OS | macOS + Linux | **Linux only** |
| Acceleration | HVF / KVM (TCG fallback on Linux) | **KVM required** (no TCG) |
| Catalog cloud images | First-class | Converted raw or custom rootfs; not drop-in |
| Guest kernel | QEMU/UEFI path | Separate **vmlinux** (`kernel_path`) |
| SSH + hostfwd / `-P` | Yes | **No** |
| Guest agent reachability | TCP hostfwd and/or vhost-vsock | **FC vsock UDS** only (`CONNECT`, not host AF_VSOCK) |
| 9p / virtiofs mounts | Yes | **No** (not wired) |
| Overlay network | Yes | **No** |
| Egress proxy via SLIRP | Yes | **No** host path |
| GPU (`virtio`) | Yes | **No** |
| Suspend / savevm | Yes | **Unsupported** |
| Pause / resume | QMP | FC API (when socket up) |
| Jailer / production isolation extras | N/A | **Jailer-less** experimental launch |
| `agent_transport` config | auto / tcp / vsock | Ignored (FC vsock always) |

**Out of scope for this experimental path:** CNI/TAP, SLIRP hostfwd, production jailer, and **published** (pullable) catalog FC artifacts. Catalog IDs `grain-ubuntu-fc` / `fc-kernel` are reserved scaffolding; bake + digests are the next Phase 1 work.

**Production track (multi-phase):** the full QEMU-vs-FC matrix with target phases is in [Hypervisor matrix](../../explain/hypervisor-matrix/) — **vFC-1** = host agent dial over FC vsock UDS (`CONNECT`), **vFC-2** = net/mounts (hostfwd, overlay, proxy path, shares). This guide stays the complete **experimental** operator surface (BYO + doctor + vsock); the matrix is the phase map.

## Related

- [Hypervisor matrix](../../explain/hypervisor-matrix/) — QEMU vs FC today + vFC-1 / vFC-2 targets
- [Images](../images/#firecracker-rootfs-experimental) — base images, golden bake (QEMU-oriented); FC rootfs notes
- [Networking](../networking/) — QEMU SLIRP / hostfwd (not used by FC)
- [Guest agent](../agent/) — guest agent HTTP API
- [Troubleshooting](../troubleshooting/) — doctor and logs (includes Firecracker doctor rows)
- [Configuration](../../reference/config/#firecracker-experimental) — `hypervisor`, `firecracker_binary`, `kernel_path`
- [Concepts](../../get-started/concepts/#hypervisors) — hypervisor glossary
- [Product surface](../../explain/parity/) — experimental status
