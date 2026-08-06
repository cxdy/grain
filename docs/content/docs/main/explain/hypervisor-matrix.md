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
Operator setup for the Firecracker backend (kernel, doctor, catalog pull, vsock agent).
{{< /only-need >}}

This page is the **capability snapshot** for grain’s two real hypervisors: **QEMU** (default product path) and **Firecracker** (Linux + KVM backend).

## Support policy (read this first)

| Label | Meaning |
|-------|---------|
| **FC agent production (vFC-1)** | Supported for agent-first workflows on Linux+KVM: pull `fc-kernel` / `grain-ubuntu-fc`, `grain new --wait agent`, `grain x` / `sh` / `cp` / sync / MCP tools that use the guest agent. Host dial uses Firecracker vsock UDS + `CONNECT`. |
| **FC net (vFC-2 partial)** | **Supported:** TAP + create-time `-P` / `--publish` (DNAT + SNAT to TAP HostIP), `grain fwd add/ls/rm` via host TCP proxy, optional SSH/agent TCP ports. **Still QEMU-only:** overlay L2, 9p/virtiofs mounts, SLIRP-style egress proxy. Needs `CAP_NET_ADMIN` + `/dev/net/tun`. |
| **QEMU default** | Full product path on macOS + Linux (SLIRP, publish, mounts, overlay, GPU where applicable). |

CLI `--publish` / `grain fwd` work on **both** QEMU (SLIRP hostfwd / SSH `-L`) and Firecracker (TAP DNAT / TCP proxy). Prefer agent APIs when you do not need a guest TCP port on the host.

**Production plan is multi-phase.** Firecracker is not a drop-in QEMU replacement:

| Phase | Focus | Status |
|-------|--------|--------|
| **vFC-1 (agent)** | Catalog kernel/rootfs, doctor, host UDS `CONNECT` dial, create-wait agent | **Shipped** on `main` / `fc-latest` |
| **vFC-2 (net)** | TAP + publish/fwd; overlay/mounts still later | **Partial shipped** (publish/fwd); overlay/mounts QEMU-only |
| **never** | macOS FC host, virtio GPU, QEMU-style savevm | Use QEMU |

Operator how-to: [Firecracker on Linux](../../guides/firecracker/). Product checklist: [Product surface](../parity/).

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
| **Images / rootfs** | Catalog qcow2 (`grain-ubuntu`, `ubuntu-cloud`, …) + import | Catalog **`grain-ubuntu-fc`** raw (pull `fc-latest`) or import; qcow2→raw via `qemu-img` at Start | — (vFC-1 catalog shipped) |
| **Guest kernel** | QEMU/UEFI path from image | Catalog **`fc-kernel`** → `~/.grain/kernels/vmlinux`, or `kernel_path` / import | — (vFC-1 catalog shipped) |
| **SSH + hostfwd / `-P` / `grain fwd`** | Yes (SLIRP hostfwd) | **Yes (vFC-2)** — TAP + DNAT/SNAT for create-time `-P`; live `grain fwd add` via host TCP proxy to guest IP; SSH host port allocated (sshd must exist in guest). Needs CAP_NET_ADMIN | **vFC-2 net (partial done)** |
| **Agent transport** | TCP hostfwd and/or host **AF_VSOCK** (`vhost-vsock-pci`); `agent_transport: auto\|tcp\|vsock` | **Primary:** Firecracker vsock UDS + `CONNECT` (`AgentCID`, `fc-vsock.sock`). Optional TCP DNAT to guest `:7475` when TAP is up. Create-wait / CLI / daemon proxy use vsock first | **vFC-1 agent (done)** |
| **Mounts (9p / virtiofs)** | Yes (virtiofs on Linux) | **Not wired** | later (not in vFC-2 publish scope) |
| **Overlay network** (`network: overlay`) | Yes (shared L2 between VMs) | **No** | later (QEMU-only for now) |
| **Egress proxy** (SLIRP hostfwd path) | Yes | Guest egress via TAP MASQUERADE; **no** SLIRP `10.0.2.2` proxy path | later for proxy parity |
| **Pause / resume** | QMP | FC API `PATCH /vm` (`Paused` / `Resumed`) when API socket is up | — (today) |
| **Suspend / savevm** | QEMU savevm / restore | **Unsupported** (`savevm is not supported for firecracker`) | **never** for QEMU-style savevm; FC snapshot API is a separate future decision |
| **Clone** (`grain clone` / `new --clone`) | Offline copy of stopped **persistent** VM (qcow2 overlay + meta) | Same manager path for stopped persistent disks; not FC-specific. Guest networking/agent ports reallocated on next start | — (manager-level today; not a VMM feature) |
| **MCP** | Full tool surface when daemon + guest agent reachable | Control-plane MCP works (list/create lifecycle where VM exists); **agent-backed tools** need a working FC agent dial | **vFC-1 agent** for guest tools |
| **Remote API / SDKs** | Unix socket + TCP API; guest ops via agent or daemon proxy | Control plane is hypervisor-agnostic; guest exec/shell/cp over API still need agent reachability | **vFC-1 agent** for guest ops |
| **GPU** (`virtio` / `--gpu`) | Yes | **No** | **never** (use QEMU) |
| **Jailer / production isolation extras** | N/A | **Jailer-less** experimental launch | later (not vFC-1 / vFC-2 scope) |

## Agent path detail (vFC-1)

Firecracker Start wires the guest and host agent path:

1. `SSHPort = 0`, `AgentPort = 0` (no TCP hostfwd)
2. `AgentCID` allocated (same CID allocator as QEMU vsock)
3. Vsock UDS at `~/.grain/vms/<name>/fc-vsock.sock`
4. Host `agent.Dial` / create-wait / daemon proxy: connect UDS → `CONNECT 7475\n` → HTTP to grain-agent

That is **not** QEMU’s host AF_VSOCK (`/dev/vhost-vsock`). SSH remains a **QEMU** bootstrap path; FC agent access is vsock-first (baked agent in `grain-ubuntu-fc`; no SSH deploy on FC).

## What each phase does *not* include

- **vFC-1** does not add SLIRP, publish ports, overlay, proxy hostfwd, or mounts — those stay **QEMU-only** until vFC-2.
- **vFC-2** does not promise macOS Firecracker, GPU, or QEMU savevm semantics.
- **Jailer** and multi-host CNI remain out of the agent-production bar (optional later; single-tenant only).

## Related

- [Firecracker on Linux](../../guides/firecracker/) — experimental operator path
- [Product surface](../parity/) — done / experimental checklist
- [Concepts](../../get-started/concepts/#hypervisors) — hypervisor glossary
- [Networking](../../guides/networking/) — QEMU SLIRP / hostfwd model
- [Guest agent](../../guides/agent/) — agent HTTP API
- [Architecture](../architecture/) — daemon / hypervisor / agent fit
