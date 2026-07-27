---
title: "Remote sandbox host (team box)"
description: "Run grain as a service on a shared Linux (or Mac) machine so developers spin up sandboxes remotely."
---

grain is local-first, but a single **beefy host** can serve a team: one daemon, many sandboxes, developers connect over SSH or the HTTP API. This guide covers a practical, honest setup for that pattern.

## What this is (and is not)

| This is | This is not |
|---------|-------------|
| A shared **lab / agent / CI sandbox** machine | Multi-tenant hard isolation for untrusted customers |
| One control plane with **resource caps** and a **token** | Per-user RBAC, quotas, or billing |
| Developers using **SSH + CLI** or **SDKs over HTTP** | A drop-in Kubernetes cluster |
| Guest isolation via microVMs | A substitute for your VPN, firewall, and OS patching |

If you need true multi-tenant SaaS isolation, put grain behind stronger controls (or separate hosts per trust domain). See [Security model]({{ '/explain/security/' | relative_url }}).

## Architecture

```text
Developer laptop                    Shared host (Linux + KVM recommended)
┌─────────────────────┐             ┌──────────────────────────────────┐
│ grain CLI (optional)│── SSH ────►│ grain daemon (systemd)           │
│ or curl / SDK       │── HTTPS ──►│   unix socket + TCP API :7474     │
│                     │            │   QEMU microVMs                   │
│ browser → :8080     │◄─ tunnel ──│   hostfwd 127.0.0.1:ssh/agent/app │
└─────────────────────┘             └──────────────────────────────────┘
```

**Important behaviors on a remote host:**

1. **CLI talks to a unix socket on the machine where it runs.** Today the grain CLI does not dial a remote TCP URL by itself. Team workflows either **SSH to the host and run `grain` there**, or call the **HTTP API / SDKs** from anywhere.
2. **Published ports bind to `127.0.0.1` on the grain host**, not the public internet. Reach guest apps via SSH tunnels (below).
3. **Mounts (`-v`) are host paths on the sandbox machine**, not paths on the developer laptop. Sync code with `git`, `grain cp`, or an agent checkout on the host.

## 1. Prepare the host

Supported hosts only: **Linux or macOS** (not Windows/WSL). For a team box, prefer **Linux with KVM**.

```bash
# Ubuntu/Debian example
sudo apt-get update
sudo apt-get install -y qemu-system qemu-utils curl git

# install grain (release binary or build)
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
grain doctor
```

Pull a golden image once so creates stay fast:

```bash
grain image pull grain-ubuntu   # or ubuntu-cloud if golden not published yet
grain image ls
```

Create a dedicated service user (recommended on multi-user Linux):

```bash
sudo useradd --system --home /var/lib/grain --shell /usr/sbin/nologin grain || true
sudo mkdir -p /var/lib/grain /etc/grain
sudo chown -R grain:grain /var/lib/grain
```

## 2. Daemon config for a shared host

Write `/etc/grain/config.yaml` (paths are examples — adjust for a single-user home install with `~/.grain/config.yaml` if you prefer).

```yaml
data_dir: /var/lib/grain
socket: /var/lib/grain/grain.sock
# TCP API: bind loopback for SSH tunnels / reverse proxy only (recommended).
# Use a private NIC IP only if you understand the exposure.
api: 127.0.0.1:7474
# REQUIRED if any non-local process can reach the API
api_token: "replace-with-long-random-secret"

image: auto                 # prefer grain-ubuntu when Ready
cpus: 2
memory_mb: 2048
disk_gb: 16
ready_timeout: 3m
log_level: info

# Protect the box from runaway creates
max_vms: 16
max_cpus_total: 32
max_memory_mb_total: 65536
max_cpus_per_vm: 8
max_memory_mb_per_vm: 16384

# Optional: named defaults for teammates
profiles:
  agent:
    cpus: 4
    memory_mb: 8192
    disk_gb: 20
    image: grain-ubuntu
    mounts:
      - {host: /var/lib/grain/workspaces, guest: "/work"}
  ci:
    cpus: 2
    memory_mb: 4096
    persistent: false
```

Generate a token:

```bash
openssl rand -hex 32
# put the value in api_token above; share only via your secret store
```

## 3. Run as a service (systemd)

Example unit (also in the repo as [`deploy/systemd/grain.service`](https://github.com/cxdy/grain/blob/main/deploy/systemd/grain.service)):

```ini
[Unit]
Description=grain microVM control plane
After=network-online.target
Wants=network-online.target

[Service]
Type=simple
User=grain
Group=grain
ExecStart=/usr/local/bin/grain --config /etc/grain/config.yaml up --fg
Restart=on-failure
RestartSec=2
# Optional hardening (tune for your environment)
NoNewPrivileges=true
# QEMU needs access to /dev/kvm when using KVM:
# SupplementaryGroups=kvm
# DeviceAllow=/dev/kvm rw

[Install]
WantedBy=multi-user.target
```

```bash
sudo install -m 644 deploy/systemd/grain.service /etc/systemd/system/grain.service
# ensure binary path matches ExecStart; add grain user to group kvm if needed:
# sudo usermod -aG kvm grain
sudo systemctl daemon-reload
sudo systemctl enable --now grain
sudo systemctl status grain
curl -sS -H "Authorization: Bearer $GRAIN_TOKEN" http://127.0.0.1:7474/healthz
```

**macOS team Mac mini:** use `grain up` under a dedicated user, or a LaunchAgent with `up --fg`; prefer Linux+KVM for density.

### Optional: egress proxy on the host

If sandboxes should not call arbitrary internet endpoints:

```bash
# as the same user / data_dir the daemon uses
grain --config /etc/grain/config.yaml proxy up
grain --config /etc/grain/config.yaml proxy allow --host registry-1.docker.io
# create VMs with: grain new --proxy …
```

See [Egress proxy]({{ '/guides/proxy/' | relative_url }}).

## 4. How developers connect

### Pattern A — SSH + CLI on the host (simplest)

Give each developer SSH access to the host (or a jump box). They run grain **on the host**:

```bash
ssh sandbox.example.com

export GRAIN_TOKEN=…   # if set in host config; CLI also reads config api_token
grain --config /etc/grain/config.yaml ls
grain --config /etc/grain/config.yaml new --profile agent -n alice-dev
grain --config /etc/grain/config.yaml sh alice-dev
```

Tips:

- Prefix with a shell alias: `alias grain='grain --config /etc/grain/config.yaml'`
- Use **unique VM names** (`$USER-…`) so people do not clobber each other
- Prefer **ephemeral** sandboxes for agents; `-p` only for long-lived labs

### Pattern B — SSH tunnel + HTTP API / SDKs (laptop automation)

Keep the API on loopback; tunnel it to the laptop:

```bash
# laptop
ssh -N -L 7474:127.0.0.1:7474 sandbox.example.com
export GRAIN_TOKEN=…
curl -sS -H "Authorization: Bearer $GRAIN_TOKEN" http://127.0.0.1:7474/vms
```

**Go**

```go
c, err := client.DialHTTP("http://127.0.0.1:7474", os.Getenv("GRAIN_TOKEN"))
```

**TypeScript**

```ts
const grain = new GrainClient({
  baseURL: "http://127.0.0.1:7474",
  token: process.env.GRAIN_TOKEN,
});
```

**Python**

```python
grain = GrainClient.http("http://127.0.0.1:7474", token=os.environ["GRAIN_TOKEN"])
grain.health()
```

### Pattern C — Reverse proxy with TLS (optional)

If many automation clients should hit `https://grain.internal` without a tunnel:

1. Keep grain on `127.0.0.1:7474` with `api_token` set  
2. Terminate TLS on Caddy/nginx/Envoy in front  
3. Forward to loopback; do **not** expose unauthenticated grain on `0.0.0.0`  

Example Caddy snippet:

```caddy
grain.internal {
  reverse_proxy 127.0.0.1:7474
}
```

Clients still send `Authorization: Bearer <api_token>`. Consider mTLS or SSO at the proxy for company networks; grain’s token is a single shared secret, not per-user OAuth.

## 5. Reaching services inside a sandbox

Guest ports published with `-P` / `fwd add` listen on **`127.0.0.1` on the grain host**. From a laptop:

```bash
# on host: grain new -n web -P 8080:80 …
# laptop: forward host loopback 8080 to local 8080
ssh -N -L 8080:127.0.0.1:8080 sandbox.example.com
open http://127.0.0.1:8080
```

SSH into a guest (via host):

```bash
# on host
grain fwd ls web          # note SSH host port
grain sh web              # preferred
```

## 6. Workspaces and mounts

Put shared or per-user trees **on the host**, then mount into guests:

```bash
sudo mkdir -p /var/lib/grain/workspaces/{alice,bob}
# developers git clone into their directory on the host (or CI checks out there)

grain new --profile agent -n alice-1 \
  -v /var/lib/grain/workspaces/alice:/work
```

Do **not** expect `-v /Users/alice/src:/work` from a laptop to work against a remote daemon — that path must exist on the **host**.

Sync alternatives:

- `git` on the host  
- `grain cp` / SDK `put_file` / `get_file`  
- CI job checks out then `grain new` + exec  

## 7. Operational checklist

| Item | Recommendation |
|------|----------------|
| Auth | Always set `api_token` on shared hosts |
| API bind | Prefer `127.0.0.1` + SSH tunnel or TLS reverse proxy |
| Caps | Set `max_vms`, `max_cpus_total`, `max_memory_mb_total` |
| Images | Pre-pull `grain-ubuntu` (and workload images you care about) |
| Disk | Monitor `/var/lib/grain`; prune stopped VMs and old overlays |
| Logs | `journalctl -u grain -f`; per-VM `grain logs <name>` |
| Upgrades | Stop creates, `systemctl restart grain`, re-pull golden if agent major-bumps |
| Backups | Persistent VM disks under `data_dir/vms/` if you care about labs |
| Trust | One shared token = one trust domain; split hosts for hostile tenants |

### Example day-to-day flows

**Coding agent on the team box**

```bash
ssh sandbox
cd /var/lib/grain/workspaces/alice && git pull
grain new --profile agent -n alice-agent -v "$PWD:/work" --wait agent
grain x alice-agent -- bash -lc 'cd /work && ./run-agent.sh'
grain cp alice-agent:/work/out/report.md ./report.md
grain rm alice-agent
```

**CI job (API from runner)**

```bash
# runner has GRAIN_TOKEN and SSH tunnel or internal HTTPS URL
curl -sS -X POST -H "Authorization: Bearer $GRAIN_TOKEN" \
  -H 'Content-Type: application/json' \
  -d '{"name":"ci-'$BUILD_ID'","persistent":false,"cpus":2,"memory_mb":4096}' \
  "http://127.0.0.1:7474/vms?wait=agent&timeout=5m"
# exec tests via POST /vms/{name}/exec … then DELETE
```

See also [CI ephemeral recipe]({{ '/guides/recipes/ci-ephemeral/' | relative_url }}) and [HTTP API]({{ '/reference/api/' | relative_url }}).

## 8. Limitations to plan around

- **No per-user auth** in the daemon — one Bearer token (or network ACL) for the control plane  
- **No remote CLI URL** yet — use SSH session or SDKs  
- **SLIRP networking** — simple and safe default; not a full bridge/VLAN fabric  
- **Density** — bound by host RAM/CPU and QEMU overhead; use caps and ephemeral VMs  
- **Ephemeral VMs die with daemon policy** — know what happens on `systemctl restart` for non-persistent instances  

## See also

- [Configuration]({{ '/reference/config/' | relative_url }}) — all knobs  
- [Security model]({{ '/explain/security/' | relative_url }}) — trust boundaries  
- [Resource caps]({{ '/reference/config/' | relative_url }}) — `max_*` fields  
- [Networking]({{ '/guides/networking/' | relative_url }}) — publish / fwd / SLIRP  
- [Go]({{ '/reference/go-sdk/' | relative_url }}) · [TypeScript]({{ '/reference/typescript-sdk/' | relative_url }}) · [Python]({{ '/reference/python-sdk/' | relative_url }}) SDKs  
