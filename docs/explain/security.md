---
title: Security model
description: What grain isolates, what it trusts, and how proxy and secrets fit.
---

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

### Shared / remote hosts

Running grain on a team machine so developers create sandboxes remotely is a supported **ops pattern**, not multi-tenant SaaS.

- Set `api_token`; daemon **will not** bind a non-loopback `api` without one  
- Prefer `api: 127.0.0.1:7474` + SSH tunnel or TLS reverse proxy  
- **Firewall** port 7474 (and egress proxy 3128) — do not leave control plane open to the internet  
- Remote CLI: `GRAIN_API` / `--api` + `GRAIN_TOKEN`  
- Resource caps; published ports stay on host loopback  

Full guide: [Remote sandbox host]({{ '/guides/remote-host/' | relative_url }}).

## Images

Only pull images from sources you trust (`ubuntu-cloud`, `grain-ubuntu` from your releases, `alpine-cloud` from Alpine). Verify SHA256 when provided.
