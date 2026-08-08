---
title: "Overlay network (guest↔guest L2)"
description: Share an L2 segment across grain VMs with network overlay mode.
section: guides
keywords:
  - overlay
  - guest to guest
  - multi-VM
  - multicast
  - L2
  - network
---

{{< only-need href="guides/networking/" >}}
Host→guest ports and SLIRP only — no guest↔guest fabric needed.
{{< /only-need >}}

Default networking is **SLIRP** (`user`): each VM is isolated from peers; host→guest traffic uses port forwards only.

Set **`network: overlay`** when guests must talk to each other. grain adds a second NIC via QEMU’s multicast socket backend so VMs on the same host join L2 segment `230.0.0.1:4242`. SLIRP stays for SSH and published ports.

## Enable

**Per VM**

```bash
grain new -n a --network overlay --wait agent
grain new -n b --network overlay --wait agent
```

**Config default** (`~/.grain/config.yaml`)

```yaml
network: overlay   # or slirp
```

## What you get

| Interface | Role |
|-----------|------|
| First NIC (SLIRP) | Host access, `hostfwd`, proxy via `10.0.2.2` |
| Second NIC (overlay) | Guest↔guest on the shared multicast LAN |

Inside the guest, configure addresses on the second interface yourself (static IP, mDNS, etc.). grain does not run DHCP on the overlay.

## Security note

**Overlay places every participating VM on one shared L2.** There is no guest↔guest firewall from grain.

The guest agent listens on **`:7475` without authentication**. On SLIRP-only VMs that is reachable from the host only via **loopback hostfwd** (or vsock). On overlay, a peer can open TCP to another guest’s agent on the overlay interface and run exec/shell/fs as that agent.

| Network mode | Guest↔guest L2 | Peer can hit other guests’ `:7475` agent? |
|--------------|----------------|-------------------------------------------|
| `slirp` (default) | No | No (agent only via host loopback hostfwd / vsock) |
| `overlay` | Yes (multicast) | **Yes** — treat all overlay VMs as one trust domain |

**Do:** use overlay for cooperative multi-VM labs (k3s nodes, service meshes, integration tests) under one operator.  
**Don’t:** put untrusted or multi-tenant workloads on the same overlay; don’t publish agent port 7475 beyond loopback.

Host→guest SSH and published ports still use SLIRP hostfwd bound to `127.0.0.1`. Overlay does not open those to the LAN. Full trust model: [Security model](../../explain/security/#guest-agent-trust-model).

When you create with `--network overlay`, the daemon logs a one-time **Warn** that peers share L2 and can reach each other’s agents.

## Limits

- Same host only (multicast socket, not a routable multi-host fabric)
- Firewall/OS multicast restrictions can block the overlay
- Firecracker backend does not use this path
- **No multi-tenant isolation between overlay peers** (see security note above)

## See also

- [Networking & ports](../networking/) — SLIRP and hostfwd  
- [Security model](../../explain/security/) — agent trust and overlay isolation  
- [Guest agent](../agent/#trust-model) — who may dial `:7475`  
