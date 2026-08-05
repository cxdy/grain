---
title: "Security model (trust, proxy, secrets)"
description: What grain isolates, what it trusts, and how proxy and secrets fit.
section: explain
keywords:
  - security
  - isolation
  - trust
  - API token
  - secrets
  - proxy
  - multi-tenant
---

{{< only-need href="guides/remote-host/" >}}
Team/shared host setup (token, bind, firewall) is covered in Remote sandbox host.
{{< /only-need >}}

## What grain gives you

- **Hardware virtualization boundary** between host and guest (QEMU/HVF or Firecracker/KVM)  
- **Ephemeral disks** by default so experiments do not linger  
- **Optional egress filtering** so guests only reach allowed HTTP(S) destinations  
- **Optional API tokens** if the TCP API is exposed beyond localhost  

## What grain does not claim

- Multi-tenant hard isolation between untrusted co-tenants on one host without additional hardening  
- A substitute for your OS firewall and disk encryption  
- Perfect secrecy if you inject secrets **into** the guest filesystem — the guest process can read them  

## Trust boundaries

```text
You (operator)
  → host grain daemon (trusted)
      → hypervisor
          → guest (less trusted workload / agent code)
  → host egress proxy (trusted policy + secrets)
          → internet
```

## Secrets: two patterns

1. **Inject** — materialize a file in the guest. Use for TLS keys and app config files.  
2. **Proxy inject** — guest uses a placeholder path to the proxy; real `Authorization` is added on the host. Prefer this for cloud API tokens.

## Network exposure

- Default API bind `127.0.0.1` is intentional  
- Proxy default `0.0.0.0:3128` is intentional so SLIRP guests can reach `10.0.2.2` — restrict with host firewall if the machine is multi-user or public  
- Set `api_token` if anything other than local clients can reach the TCP API  

### Guest agent trust model

`grain-agent` is an **unauthenticated** HTTP server inside each guest (default listen **`:7475`**). Anyone who can open TCP (or vsock) to that port can exec, shell, and read/write files as the agent process (often root or uid 1000). Isolation is therefore about **who can reach the agent**, not agent-side tokens.

| Path | Who may reach the agent | Auth |
|------|-------------------------|------|
| **Default (SLIRP)** | Host process dials `127.0.0.1:<agent_port>` hostfwd → guest `:7475` | Hostfwd is **loopback-only** — other machines cannot hit it |
| **Remote CLI / SDK** | Client → **daemon API** (Bearer / unix socket) → daemon dials agent on the host | **Daemon is authenticated**; agent itself still has no token |
| **virtio-vsock** | Host kernel path to guest CID:7475 | Same trust as local host processes with vsock access |
| **`network: overlay`** | **Any peer VM on the shared L2** can dial guest `:7475` on the overlay NIC | **None** — peers can control each other’s agents |

Implications:

- Do **not** publish guest port 7475 with `-P` / hostfwd to non-loopback, and do not re-bind hostfwds as `0.0.0.0`.  
- Prefer remote access via authenticated API proxy (`GRAIN_API` + `GRAIN_TOKEN`), not by tunneling raw agent ports.  
- Treat every guest on an overlay as the **same trust domain**.

Details: [Guest agent](../guides/agent/#trust-model), [Overlay network](../guides/networking-overlay/#security-note).

### Overlay network (shared L2)

Default `network: slirp` keeps each VM on an isolated user-net. **`network: overlay`** adds a second NIC on a **shared multicast L2** (`230.0.0.1:4242`) so guests can talk to each other. That is intentional for multi-VM labs — and it means **guest-to-guest isolation is gone** for anything listening on all interfaces (including the agent on `:7475`).

Use overlay only among workloads that may fully trust one another. Untrusted multi-tenant guests must stay on **slirp** (or separate hosts). Guide: [Overlay network](../guides/networking-overlay/).

### Shared / remote hosts

Running grain on a team machine so developers create sandboxes remotely is a supported **ops pattern**, not multi-tenant SaaS.

- Set `api_token`; daemon **will not** bind a non-loopback `api` without one  
- Prefer `api: 127.0.0.1:7474` + SSH tunnel or TLS reverse proxy  
- **Firewall** port 7474 (and egress proxy 3128) — do not leave control plane open to the internet  
- Remote CLI: `GRAIN_API` / `--api` + `GRAIN_TOKEN`  
- Resource caps; published ports stay on host loopback  
- Avoid `network: overlay` across different users’ sandboxes on a shared host  

Happy path: [Remote lab](../guides/remote-lab/). Ops: [Remote sandbox host](../guides/remote-host/).

## Images

Only pull images from sources you trust (`ubuntu-cloud`, `grain-ubuntu` from your releases, `alpine-cloud` from Alpine). Verify SHA256 when provided.
