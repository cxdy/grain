---
title: "Hypervisor matrix (QEMU vs Firecracker)"
description: "Capability matrix: QEMU (default) vs Firecracker today, and production-track target phases (vFC-1 agent, vFC-2 net/mounts)."
section: explain
keywords:
  - Firecracker
  - QEMU
  - hypervisor
  - matrix
  - parity
  - vsock
  - production
  - vFC-1
  - vFC-2
---

{{< only-need href="guides/firecracker/" >}}
Operator setup for the experimental Firecracker backend (kernel, doctor, raw rootfs).
{{< /only-need >}}

This page is the **capability snapshot** for grain’s two real hypervisors: **QEMU** (default product path) and **Firecracker** (experimental Linux backend). It is **documentation only** — no VMM code changes land with this matrix.

**Production plan is multi-phase.** Firecracker is intentionally not a single “flip the switch” replacement for QEMU. The track is:

| Phase | Focus |
|-------|--------|
| **Today** | Experimental FC launch: Linux+KVM, raw rootfs, vsock device configured, jailer-less |
| **vFC-1 (agent)** | Host↔guest **agent path** over Firecracker vsock UDS (`CONNECT` protocol) so `grain x` / `sh` / `cp` / create-wait agent work without SSH |
| **vFC-2 (net)** | Networking and mounts parity path: hostfwd/publish, overlay, egress proxy host path, 9p/virtiofs (or FC equivalents) |
| **never** | Not planned for Firecracker (use QEMU), or permanently out of scope for this backend |

Operator how-to (config, doctor, layout): [Firecracker on Linux](../../guides/firecracker/). Product checklist: [Product surface](../parity/).

## How to read the matrix

| Column | Meaning |
|--------|---------|
| **QEMU** | Default backend (`hypervisor: qemu`) on macOS and Linux |
| **Firecracker (today)** | What `hypervisor: firecracker` does **now** in tree |
| **Target phase** | Where full or usable FC support is aimed: `vFC-1 agent`, `vFC-2 net`, `never`, or `—` if already good enough today |

Statuses in the FC column are intentional honesty, not TODOs disguised as features.

## Capability matrix

| Capability | QEMU | Firecracker (today) | Target phase |
|------------|------|---------------------|--------------|
| **Host OS** | macOS + Linux | **Linux only** | **never** (FC is Linux/KVM-only) |
| **Acceleration / KVM** | HVF (macOS), KVM (Linux), TCG fallback on Linux | **KVM required** (`/dev/kvm` RDWR); no TCG | — (hard requirement today) |
| **Images / rootfs** | Catalog qcow2 (`grain-ubuntu`, `ubuntu-cloud`, …) + import | Raw rootfs preferred; qcow2 converted via `qemu-img` at Start; not drop-in catalog | later (catalog FC rootfs); not vFC-1 |
| **Guest kernel** | QEMU/UEFI path from image | Separate **vmlinux** (`kernel_path` or `~/.grain/kernels/vmlinux`) | — (operator-supplied today) |
| **SSH + hostfwd / `-P` / `grain fwd`** | Yes (SLIRP hostfwd) | **No** — `SSHPort` / published ports not configured | **vFC-2 net** |
| **Agent transport** | TCP hostfwd and/or host **AF_VSOCK** (`vhost-vsock-pci`); `agent_transport: auto\|tcp\|vsock` | On Start: `SSHPort=0`, `AgentPort=0`, **`AgentCID` set**, Firecracker `vsock` with `uds_path` = `vms/<name>/fc-vsock.sock`. Host side is **not** AF_VSOCK: clients must dial the UDS and send `CONNECT <port>\n` ([FC vsock docs](https://github.com/firecracker-microvm/firecracker/blob/main/docs/vsock.md)). Guest agent still listens on AF_VSOCK **7475**. Host `agent.Dial` AF_VSOCK path does **not** speak CONNECT yet | **vFC-1 agent** |
| **Mounts (9p / virtiofs)** | Yes (virtiofs on Linux) | **Not wired** | **vFC-2 net** (mounts ride the net/FS phase) |
| **Overlay network** (`network: overlay`) | Yes (shared L2 between VMs) | **No** | **vFC-2 net** |
| **Egress proxy** (SLIRP hostfwd path) | Yes | **No** host path (no SLIRP/hostfwd) | **vFC-2 net** |
| **Pause / resume** | QMP | FC API `PATCH /vm` (`Paused` / `Resumed`) when API socket is up | — (today) |
| **Suspend / savevm** | QEMU savevm / restore | **Unsupported** (`savevm is not supported for firecracker`) | **never** for QEMU-style savevm; FC snapshot API is a separate future decision |
| **Clone** (`grain clone` / `new --clone`) | Offline copy of stopped **persistent** VM (qcow2 overlay + meta) | Same manager path for stopped persistent disks; not FC-specific. Guest networking/agent ports reallocated on next start | — (manager-level today; not a VMM feature) |
| **MCP** | Full tool surface when daemon + guest agent reachable | Control-plane MCP works (list/create lifecycle where VM exists); **agent-backed tools** need a working FC agent dial | **vFC-1 agent** for guest tools |
| **Remote API / SDKs** | Unix socket + TCP API; guest ops via agent or daemon proxy | Control plane is hypervisor-agnostic; guest exec/shell/cp over API still need agent reachability | **vFC-1 agent** for guest ops |
| **GPU** (`virtio` / `--gpu`) | Yes | **No** | **never** (use QEMU) |
| **Jailer / production isolation extras** | N/A | **Jailer-less** experimental launch | later (not vFC-1 / vFC-2 scope) |

## Agent path detail (why vFC-1 exists)

Firecracker Start already wires the **guest** side:

1. `SSHPort = 0`, `AgentPort = 0` (no TCP hostfwd)
2. `AgentCID` allocated (same CID allocator as QEMU vsock)
3. Vsock UDS at `~/.grain/vms/<name>/fc-vsock.sock`

That is **not** the same as QEMU’s host AF_VSOCK (`/dev/vhost-vsock`). A host client must:

```text
connect(unix: …/fc-vsock.sock)
write("CONNECT 7475\n")
# then HTTP to grain-agent
```

Until the host dial stack speaks that **CONNECT** protocol (and create-wait / CLI use it), “agent configured” ≠ “agent usable from the CLI.” That gap is **vFC-1**. SSH remains a QEMU-first bootstrap path; FC production agent access is vsock-first.

## What each phase does *not* include

- **vFC-1** does not add SLIRP, publish ports, overlay, proxy hostfwd, or mounts.
- **vFC-2** does not promise macOS Firecracker, GPU, or QEMU savevm semantics.
- **Jailer**, polished **catalog FC images**, and CNI/TAP production networking may land in later production work beyond vFC-2; they are **not** implied by this matrix’s first two phases.

## Related

- [Firecracker on Linux](../../guides/firecracker/) — experimental operator path
- [Product surface](../parity/) — done / experimental checklist
- [Concepts](../../get-started/concepts/#hypervisors) — hypervisor glossary
- [Networking](../../guides/networking/) — QEMU SLIRP / hostfwd model
- [Guest agent](../../guides/agent/) — agent HTTP API
- [Architecture](../architecture/) — daemon / hypervisor / agent fit
