---
title: "Remote sandbox host (team box)"
description: "Run grain as a service on a shared machine; connect with the CLI over HTTP, SSH tunnels, or SDKs — with firewall and token rules."
section: guides
keywords:
  - remote
  - GRAIN_API
  - token
  - team
  - ssh tunnel
---

{{< only-need href="get-started/quickstart/" >}}
Run everything on your laptop first — remote host is optional.
{{< /only-need >}}

{{< only-need href="guides/remote-lab/" >}}
Day-to-day host + laptop coding lab (token, tunnel, `remote-coding`, sync, port tunnels).
{{< /only-need >}}

grain is local-first, but a single **beefy host** can serve a team: one daemon, many sandboxes, developers connect over **SSH**, the **remote CLI** (`GRAIN_API` / `--api`), or **SDKs**. This guide is the **ops** companion (systemd, firewall, reverse proxy, SDK patterns). For the shortest coding-lab path, start with [Remote lab happy path](../remote-lab/).

## What this is (and is not)

| This is | This is not |
|---------|-------------|
| A shared **lab / agent / CI sandbox** machine | Multi-tenant hard isolation for untrusted customers |
| One control plane with **resource caps** and a **token** | Per-user RBAC, quotas, or billing |
| Developers using **SSH + CLI**, **remote CLI**, or **SDKs** | A drop-in Kubernetes cluster |
| Guest isolation via microVMs | A substitute for your VPN, firewall, and OS patching |

If you need true multi-tenant SaaS isolation, put grain behind stronger controls (or separate hosts per trust domain). See [Security model](../../explain/security/).

## Architecture

```text
Developer laptop                         Shared host (Linux + KVM recommended)
┌──────────────────────────┐             ┌──────────────────────────────────┐
│ grain CLI                │── HTTPS ──►│ grain daemon (systemd)           │
│  GRAIN_API + GRAIN_TOKEN │             │   unix socket + TCP API          │
│ or curl / Go/TS/Python   │             │   QEMU microVMs                  │
│                          │── SSH ────►│   shell on host (ops / image pull)│
│ browser → :8080          │◄─ tunnel ──│   hostfwd 127.0.0.1 only          │
└──────────────────────────┘             └──────────────────────────────────┘
```

**Behaviors to remember:**

1. **Default CLI** talks to a **unix socket on the machine where it runs**.
2. **Remote CLI** (`GRAIN_API` or `--api`) talks **HTTP** to the daemon. Guest agent ops (exec, shell, cp, fs) are **proxied through the API** — you do not need hostfwd ports on your laptop. The guest agent itself stays unauthenticated on loopback hostfwd; the **daemon token** is the remote gate.
3. **Published ports** still bind to **`127.0.0.1` on the grain host**. Reach apps with SSH tunnels.
4. **Mounts (`-v`)** are paths on the **sandbox host**, not the laptop.
5. **`grain up` / `down` / `image` / `proxy` / `logs`** are **local-host only** — run them on the server (or over SSH).
6. **`network: overlay`** joins VMs on a shared L2; peers can reach each other’s unauthenticated guest agents — do not mix untrusted tenants on one overlay ([security note](../networking-overlay/#security-note)).

## Security model (read this first)

### Control plane is powerful

Whoever can call authenticated `POST /vms` can create VMs, exec as root in guests (depending on image), and read secrets the host injects. Treat the API like **root on a shared lab**.

### Hard rules grain enforces

| Rule | Behavior |
|------|----------|
| **Non-loopback bind without token** | Daemon **refuses to start** if `api` is not loopback and `api_token` is empty |
| **Remote CLI to non-loopback** | CLI **requires** `GRAIN_TOKEN` / `api_token` |
| **Cleartext HTTP transport** | Daemon serves HTTP only; Bearer tokens on non-loopback `http://` are sniffable — prefer SSH tunnel to `127.0.0.1` or TLS reverse proxy (`https://`). CLI warns once; silence with `GRAIN_INSECURE_HTTP=1` |
| **Unix socket mode** | Socket is `0600`; still prefer token if untrusted local users share the host |
| **`GET /healthz`** | Always unauthenticated (liveness only) |

### Firewall checklist (required for production-ish hosts)

Do **not** expose an unauthenticated or world-reachable grain API.

**1. Prefer loopback + SSH tunnel (simplest, safest default)**

```yaml
# /etc/grain/config.yaml on the server
api: 127.0.0.1:7474
api_token: "long-random-secret"
```

```bash
# laptop
ssh -N -L 7474:127.0.0.1:7474 sandbox.example.com
export GRAIN_API=http://127.0.0.1:7474
export GRAIN_TOKEN=long-random-secret
grain ls
```

Host firewall: **no** inbound 7474 from the internet.

**2. If you must bind a LAN or public interface**

```yaml
api: 0.0.0.0:7474          # or a private NIC IP
api_token: "long-random-secret"   # REQUIRED — daemon will not start without it
```

Then lock the port down:

```bash
# Example: nftables / ufw — allow only VPN / bastion CIDR
sudo ufw default deny incoming
sudo ufw allow OpenSSH
sudo ufw allow from 10.8.0.0/24 to any port 7474 proto tcp   # VPN clients only
sudo ufw enable

# Or iptables sketch:
# iptables -A INPUT -p tcp --dport 7474 -s 10.8.0.0/24 -j ACCEPT
# iptables -A INPUT -p tcp --dport 7474 -j DROP
```

**Never** leave `0.0.0.0:7474` open on a public cloud security group without:

- a strong `api_token`, **and**
- SG/NACL restricted to admin/VPN CIDRs, **and** preferably TLS reverse proxy.

**3. TLS reverse proxy (company HTTPS)**

Keep grain on loopback; terminate TLS on Caddy/nginx/Envoy:

```caddy
grain.internal {
  reverse_proxy 127.0.0.1:7474
}
```

```bash
export GRAIN_API=https://grain.internal
export GRAIN_TOKEN=…
```

Add mTLS or SSO at the proxy if you need per-user identity — grain’s token is a **shared secret**, not OAuth.

**4. Other host surfaces**

| Surface | Exposure | Notes |
|---------|----------|--------|
| SSH to host | Admin / developers | Prefer keys, no password, fail2ban optional |
| QEMU guest hostfwd | `127.0.0.1` only | Tunnel from laptop; do not republish as `0.0.0.0` |
| Egress proxy `:3128` | Often `0.0.0.0` for SLIRP | **Firewall carefully** — guests need it; the public internet should not |
| Metrics | Often on API port | Same auth as API when token set (except `/healthz`) |

**5. Token hygiene**

```bash
openssl rand -hex 32   # generate
# store in your secret manager; rotate by updating config + restarting grain
# distribute to CI as GRAIN_TOKEN; never commit to git
```

## 1. Prepare the host

Supported hosts: **Linux or macOS** with hardware virtualization (prefer **Linux with KVM**). Native Windows is not a host; WSL follows the same Linux virt requirements.

```bash
sudo apt-get update
sudo apt-get install -y qemu-system qemu-utils curl git

curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
grain doctor
grain image pull grain-ubuntu
```

Dedicated service user:

```bash
sudo useradd --system --home /var/lib/grain --shell /usr/sbin/nologin grain || true
sudo mkdir -p /var/lib/grain /etc/grain
sudo chown -R grain:grain /var/lib/grain
# KVM access on Linux:
# sudo usermod -aG kvm grain
```

## 2. Daemon config

`/etc/grain/config.yaml` example (loopback + token + caps):

```yaml
data_dir: /var/lib/grain
socket: /var/lib/grain/grain.sock
api: 127.0.0.1:7474
api_token: "replace-with-long-random-secret"

image: auto
cpus: 2
memory_mb: 2048
disk_gb: 16
ready_timeout: 3m
log_level: info

max_vms: 16
max_cpus_total: 32
max_memory_mb_total: 65536
max_cpus_per_vm: 8
max_memory_mb_per_vm: 16384

profiles:
  agent:
    cpus: 4
    memory_mb: 8192
    disk_gb: 20
    image: grain-ubuntu
    mounts:
      - {host: /var/lib/grain/workspaces, guest: "/work"}
```

## 3. systemd

Example unit: [`deploy/systemd/grain.service`](https://github.com/cxdy/grain/blob/main/deploy/systemd/grain.service).

```bash
sudo install -m 644 deploy/systemd/grain.service /etc/systemd/system/grain.service
sudo systemctl daemon-reload
sudo systemctl enable --now grain
sudo systemctl status grain

export GRAIN_TOKEN=…   # same as api_token
curl -sS -H "Authorization: Bearer $GRAIN_TOKEN" http://127.0.0.1:7474/healthz
curl -sS -H "Authorization: Bearer $GRAIN_TOKEN" http://127.0.0.1:7474/info
```

## 4. How developers connect

### Pattern A — SSH + local CLI on the host

```bash
ssh sandbox.example.com
export GRAIN_TOKEN=…   # if set
grain --config /etc/grain/config.yaml ls
grain --config /etc/grain/config.yaml new --profile agent -n alice-dev
grain --config /etc/grain/config.yaml sh alice-dev
```

Use unique names (`$USER-…`). Run **`grain image pull`** only on the host.

### Pattern B — Remote CLI from the laptop (recommended for day-to-day)

```bash
# Terminal 1: tunnel API (when api: 127.0.0.1:7474 on server)
ssh -N -L 7474:127.0.0.1:7474 sandbox.example.com

# Terminal 2: laptop
export GRAIN_API=http://127.0.0.1:7474
export GRAIN_TOKEN=long-random-secret

grain ls
# Prefer durable labs: ephemeral VMs are lost if the daemon restarts.
grain new --profile remote-coding --wait agent -n alice-1
# equivalent: grain new -p -c 4 -m 8192 -d 32 -i grain-ubuntu --wait agent -n alice-1
grain x alice-1 -- uname -a
grain sh alice-1                 # WebSocket via daemon proxy
grain cp ./script.sh alice-1:/tmp/script.sh
# reverse cp (guest → laptop) also uses the agent proxy:
grain cp alice-1:/tmp/out.json ./out.json
# incremental sync (agent required; same local/remote path):
grain sync push ~/proj alice-1:/work/proj
grain sync pull alice-1:/work/proj ~/proj
grain fs ls alice-1 /tmp
grain rm alice-1
```

Equivalents:

```bash
# flag instead of env
grain --api http://127.0.0.1:7474 ls

# config on laptop (~/.grain/config.yaml) — client-only knobs
# api_url: http://127.0.0.1:7474
# api_token: "…"   # or rely on GRAIN_TOKEN
```

**Priority:** `--api` flag > env `GRAIN_API` > config `api_url`.

| Works remotely | Local-only (run on host / SSH) |
|----------------|--------------------------------|
| `ls`, `new`, `rm`, `stop`, `start`, … | `up`, `down` |
| `x`, `sh` (agent), `cp`, `sync`, `fs`, `stats` | `image ls/pull/import` |
| `fwd add/rm` (metadata + host-side) | `proxy *`, `logs` (serial files) |
| secrets inject / list (host store on server) | doctor (host tools); `agent deploy` (SSH hostfwd) |

### Pattern C — SDKs / curl

```bash
curl -sS -H "Authorization: Bearer $GRAIN_TOKEN" "$GRAIN_API/vms"
```

```go
c, _ := client.DialHTTP(os.Getenv("GRAIN_API"), os.Getenv("GRAIN_TOKEN"))
```

```ts
new GrainClient({ baseURL: process.env.GRAIN_API!, token: process.env.GRAIN_TOKEN })
```

```python
GrainClient.http(os.environ["GRAIN_API"], token=os.environ["GRAIN_TOKEN"])
```

## 5. Guest ports from a laptop

```bash
# on host / remote CLI: grain new -n web -P 8080:80 …
ssh -N -L 8080:127.0.0.1:8080 sandbox.example.com
open http://127.0.0.1:8080
```

## 6. Workspaces and mounts

```bash
sudo mkdir -p /var/lib/grain/workspaces/{alice,bob}
# clone on the host, then:
grain new --profile agent -n alice-1 -v /var/lib/grain/workspaces/alice:/work
```

Laptop paths like `/Users/alice/src` do **not** exist on the server.

### Persistent labs (`-p` / `--profile remote-coding`) for remote coding

Ephemeral sandboxes (`grain new` without `-p`) are removed when the daemon restarts. For multi-session remote work, use the builtin **`remote-coding`** profile (persistent, 4 CPU / 8 GiB RAM / 32 GiB disk, `grain-ubuntu`) or pass `-p` yourself:

```bash
grain new --profile remote-coding --wait agent -n alice-dev
grain sync push ~/dev/proj alice-dev:/work/proj
# … edit over days …
grain stop alice-dev    # free RAM; disk kept
grain start alice-dev
grain sync pull alice-dev:/work/proj ~/dev/proj
```

Use `grain sync` (or reverse `grain cp`) to move trees between **laptop** and guest — mounts only expose **host** paths on the sandbox machine.

## 7. Ops checklist

| Item | Recommendation |
|------|----------------|
| Auth | Always set `api_token` on shared hosts |
| API bind | Prefer `127.0.0.1` + SSH tunnel or TLS proxy |
| Firewall | Deny 7474 from the public internet; restrict by VPN/bastion |
| Caps | `max_vms`, `max_cpus_total`, `max_memory_mb_total` |
| Images | Pre-pull golden on the **host** |
| Logs | `journalctl -u grain -f`; serial logs only on host |
| Upgrades | Drain / restart unit; re-pull golden when agent changes |
| Trust domain | One token ≈ one team; split hosts for hostile tenants |

## 8. Limitations

- Single shared Bearer token (not per-user OAuth)
- No full bridge/VLAN networking (SLIRP hostfwd)
- Density bound by host RAM/CPU
- Ephemeral VMs vanish on daemon restart — use `-p` for durable labs
- `grain agent deploy` and `image *` must run on the host (SSH hostfwd / data dir)

## See also

- [Remote lab happy path](../remote-lab/) — host + laptop coding workflow  
- [Configuration](../../reference/config/) — `api`, `api_url`, `api_token`, caps  
- [Security model](../../explain/security/)  
- [CLI reference](../../reference/cli/)  
- [Go](../../reference/go-sdk/) · [TypeScript](../../reference/typescript-sdk/) · [Python](../../reference/python-sdk/) SDKs  
