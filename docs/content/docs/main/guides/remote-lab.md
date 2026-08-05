---
title: "Remote lab happy path (host + laptop CLI)"
description: "Operator how-to: run grain on a sandbox host, dial it from your laptop with GRAIN_API, create a remote-coding lab, sync code, and tunnel published ports."
section: guides
keywords:
  - remote
  - remote-coding
  - GRAIN_API
  - GRAIN_TOKEN
  - ssh tunnel
  - sync
  - happy path
---

{{< only-need href="get-started/quickstart/" >}}
Local laptop workflow first — remote host is optional.
{{< /only-need >}}

Run a durable coding sandbox on a **Linux/macOS host**, drive it from your **laptop CLI**. One pass, host then laptop.

For systemd, caps, reverse proxy, and team ops, see [Remote sandbox host](../remote-host/).

## What you will have

| Machine | Role |
|---------|------|
| **Sandbox host** | QEMU + grain daemon; VMs live here |
| **Laptop** | `grain` CLI with `GRAIN_API` / `GRAIN_TOKEN`; no local hypervisor required |

## 1. Host: install and config

Supported host: **Linux or macOS** with hardware virtualization (prefer Linux + KVM).

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
# install QEMU if needed (grain doctor will say)
grain doctor
grain image pull grain-ubuntu
```

Write a config with a **token** and a **safe API bind**. Prefer loopback:

```yaml
# ~/.grain/config.yaml  (or /etc/grain/config.yaml for a service user)
api: 127.0.0.1:7474
api_token: "replace-with-long-random-secret"   # openssl rand -hex 32
```

| Bind | When | Requirements |
|------|------|----------------|
| `127.0.0.1:7474` | **Default choice** | Token still recommended; laptop uses `ssh -L` |
| `0.0.0.0:7474` (or LAN IP) | Direct LAN/VPN dial | **Token required** (daemon refuses without it) + host firewall to VPN/bastion only |

Never leave the API open to the public internet without token **and** network restriction (prefer TLS reverse proxy on loopback instead).

## 2. Host: start the daemon

```bash
grain up
# optional MCP on the host loopback only:
# grain up --mcp
# → http://127.0.0.1:7476/mcp  (do not bind MCP to a public interface without protection)
```

```bash
export GRAIN_TOKEN=replace-with-long-random-secret
curl -sS -H "Authorization: Bearer $GRAIN_TOKEN" http://127.0.0.1:7474/healthz
```

`grain up` / `down` / `image *` / `doctor` run **on the host** (or over SSH). They are not remote-CLI commands.

## 3. Create a durable lab

On the **host**, or from the laptop after step 4 is set up:

```bash
grain new --profile remote-coding --wait agent -n alice-dev
```

Builtin **`remote-coding`**: persistent (`-p`), 4 CPU / 8 GiB / 32 GiB, image `grain-ubuntu`. No host mounts (laptop paths are not on the daemon machine).

Equivalent without the profile:

```bash
grain new -p -c 4 -m 8192 -d 32 -i grain-ubuntu --wait agent -n alice-dev
```

## 4. Laptop: point CLI at the host

**Preferred:** tunnel the loopback API.

```bash
# terminal 1 — keep open
ssh -N -L 7474:127.0.0.1:7474 sandbox.example.com
```

```bash
# terminal 2
export GRAIN_API=http://127.0.0.1:7474
export GRAIN_TOKEN=replace-with-long-random-secret

grain ls
grain sh alice-dev
```

If the daemon already binds a reachable LAN URL (token + firewall in place):

```bash
export GRAIN_API=http://sandbox.example.com:7474
export GRAIN_TOKEN=replace-with-long-random-secret
grain ls
```

Priority: `--api` flag > `GRAIN_API` > config `api_url`.

Install the same `grain` CLI on the laptop (no QEMU required for remote-only use).

## 5. Move code with sync (not `-v` from the laptop)

```bash
grain sync push ~/proj alice-dev:/work/proj
grain x alice-dev -- bash -lc 'cd /work/proj && make test'
grain sync pull alice-dev:/work/proj ~/proj
```

| Do | Don't |
|----|--------|
| `grain sync push` / `pull` for laptop ↔ guest trees | Assume `-v /Users/you/...` works from the laptop |
| `grain cp file alice-dev:/path` for single files | Expect mounts of laptop paths on the remote host |

**`-v` / mounts are paths on the sandbox host**, not the laptop. A host-side share looks like `-v /var/lib/grain/workspaces/alice:/work` **on the machine running the daemon**. For laptop edit loops, use **`grain sync`**.

Sync and `grain fs` need a **healthy guest agent** (no scp fallback). Prefer `--wait agent` and the golden image `grain-ubuntu`.

## 6. Browser ports: host loopback + second tunnel

Published ports bind **`127.0.0.1` on the sandbox host**, not your laptop.

```bash
# create or add a forward (example: guest 3000 → host 3000)
grain new --profile remote-coding --wait agent -n web -P 3000:3000
# or on an existing VM: grain fwd add web 3000:3000

# laptop: tunnel that host loopback port
ssh -N -L 3000:127.0.0.1:3000 sandbox.example.com
# open http://127.0.0.1:3000
```

You can combine API and app tunnels:

```bash
ssh -N \
  -L 7474:127.0.0.1:7474 \
  -L 3000:127.0.0.1:3000 \
  sandbox.example.com
```

## 7. Agent deploy is local-daemon only

| Command | Remote CLI (`GRAIN_API`) |
|---------|---------------------------|
| `ls`, `new`, `rm`, `stop`, `start`, `sh`, `x`, `cp`, `sync`, `fs`, `fwd`, `stats` | Yes (agent ops via daemon proxy) |
| `up`, `down`, `image *`, `doctor`, `logs`, `proxy *` | No — run on host |
| `grain agent deploy` | **No** — local daemon only (needs SSH hostfwd on the host) |

If the agent is missing on a non-golden image: SSH to the host and run `grain agent deploy NAME`, or recreate from `grain-ubuntu` with `--wait agent`.

## 8. Security (non-negotiable)

- **Never** open API or MCP without a shared secret and network controls.
- Prefer **`api: 127.0.0.1:7474` + SSH tunnel** over public binds.
- Non-loopback `api` **requires** `api_token` or the daemon will not start.
- Remote CLI to a non-loopback URL **requires** `GRAIN_TOKEN` / `api_token`.
- Keep MCP on **`127.0.0.1`** unless you know how you will authenticate and firewall it.
- One Bearer token ≈ one trust domain; this is a team lab pattern, not multi-tenant SaaS.

## Day-to-day cheat sheet

```bash
# host (once)
grain up
grain image pull grain-ubuntu

# laptop (each session)
ssh -N -L 7474:127.0.0.1:7474 sandbox.example.com   # other terminal
export GRAIN_API=http://127.0.0.1:7474
export GRAIN_TOKEN=…

grain ls
grain new --profile remote-coding --wait agent -n alice-dev   # once
grain sync push ~/proj alice-dev:/work/proj
grain sh alice-dev
grain stop alice-dev    # free RAM; disk kept (persistent profile)
grain start alice-dev
```

## See also

- [Remote sandbox host](../remote-host/) — systemd, firewall, reverse proxy, SDK patterns  
- [Profiles](../profiles/) — builtin `remote-coding`  
- [Guest agent](../agent/) — exec, shell, sync requirements  
- [CLI reference](../../reference/cli/) — `sync`, remote env vars  
- [Configuration](../../reference/config/) — `api`, `api_url`, `api_token`  
- [Security model](../../explain/security/)  
