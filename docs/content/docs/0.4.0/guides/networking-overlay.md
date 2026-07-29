---
title: Overlay network between VMs
description: Share an L2 segment across grain VMs with network overlay mode.
section: guides
---

By default each grain VM uses **SLIRP** (`user` networking): isolated from other VMs, with host→guest port forwards.

For multi-VM labs that need guest-to-guest connectivity, set **`network: overlay`**. grain attaches a second NIC using QEMU’s multicast socket backend so VMs on the same host join a shared L2 segment (`230.0.0.1:4242`) while keeping SLIRP for SSH and published ports.

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

## Limits

- Same host only (multicast socket, not a routable multi-host fabric)
- Firewall/OS multicast restrictions can block the overlay
- Firecracker backend does not use this path

## See also

- [Networking & ports](/docs/0.4.0/guides/networking/) — SLIRP and hostfwd  
