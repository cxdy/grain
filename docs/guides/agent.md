---
title: "Guest agent"
description: "Use grain-agent for exec, shell, files, and health."
---


The guest agent is a small HTTP server that runs **inside** each Linux VM. The host CLI and daemon talk to it over either **virtio-vsock** (when the host supports it) or a QEMU SLIRP hostfwd to guest port **7475**, so common operations work without opening an interactive SSH session.

## What it provides

| Capability | Guest HTTP | Host CLI / API |
|------------|------------|----------------|
| **Health** | `GET /health` | `grain agent health [name]` · `GET /vms/{name}/agent/health` |
| **Exec** | `POST /exec` | `grain x [name] -- cmd…` · `POST /vms/{name}/exec` |
| **Shell** | `GET /shell` (WebSocket PTY) | `grain sh [name]` · prefers agent; `--ssh` / `--agent` |
| **Copy** | `PUT/GET /cp` | `grain cp` · `PUT/GET /vms/{name}/cp` |
| **Filesystem** | `/fs/*` | `grain fs ls\|stat\|mkdir\|rm` · daemon FS routes |
| **Stats** | `GET /stats` | uptime, mem, load (and disk when available); host Prometheus `/metrics` for VM counts |
| **Secrets** | `POST /secrets/materialize` | write a payload into the guest (default `/run/grain/secrets/<name>`); host store under `~/.grain/secrets` |
| **Deploy** | — | automatic SSH install of `grain-agent` when missing |

Buffered exec returns JSON (`stdout`/`stderr`/`exit_code`). Streaming exec (`buffered=false`) emits NDJSON frames: `started` → `stdout`/`stderr` → `exit`.

## Lifecycle

```text
grain new / start
    → SSH ready (default --wait ssh)
    → if image has no baked agent: SCP grain-agent-linux-$arch + systemd unit
    → wait GET /health (soft-fail unless --wait agent|userdata)
```

| Create flag | Behavior |
|-------------|----------|
| `--wait ssh` | SSH up; agent deploy is best-effort |
| `--wait agent` | create fails if agent never becomes healthy |
| `--wait userdata` | agent healthy **and** userdata marker present |

Golden images (`grain-ubuntu` via `grain image import` / bake) set `has_agent` so create prefers probing the agent before SSH deploy. See [images.md](/guides/images/).

## Deploy (host → guest)

Deploy runs only when a Linux agent binary is found. Search order (`internal/agent.LinuxBinaryPath`):

1. Directory of the running `grain` executable  
2. `bin/` next to that executable  
3. `./bin/` / `.` relative to cwd  
4. `~/.grain/agent/grain-agent-linux-$GOARCH` (data dir)

Build both arches:

```bash
just agent-linux
# → bin/grain-agent-linux-arm64
# → bin/grain-agent-linux-amd64
```

Guest install path: `/usr/local/bin/grain-agent`  
Unit: `/etc/systemd/system/grain-agent.service` (listen `:7475`)

`grain doctor` **warns** (does not fail) when the binary is missing — VMs still work SSH-only.

## CLI

### Health

```bash
grain agent health
grain agent health sbox-1
# {
#   "hostname": "sbox-1",
#   "agent_version": "0.2.0",
#   "agent_uptime_sec": 42,
#   "userdata_ran": true
# }
```

### Stats

Guest resource basics (`GET /stats` on the agent): uptime, memory total/available, load average, optional disk totals. Collected from `/proc` on Linux. Useful for agents and CI to decide whether the sandbox is healthy under load. Host-level counters live on the daemon Prometheus endpoint (`GET /metrics`, `grain_vms_*`).

### Secrets

The guest agent can materialize a secret file (`POST /secrets/materialize`) with base64 payload, optional mode/uid/gid, default path `/run/grain/secrets/<name>`. The host keeps a file-backed store under `~/.grain/secrets/` (mode `0700`) for daemon-mediated inject workflows. Prefer short-lived lab credentials; do not treat this as a full KMS.

### Exec

```bash
grain x sbox-1 -- uname -a
grain x -- id                    # single VM: name optional
grain x --agent -- true          # require agent
grain x --ssh -- true            # force SSH
```

### Interactive shell

`grain sh` prefers an interactive PTY over the guest agent WebSocket (`GET /shell`) so a shell works even when `sshd` is down, as long as the agent is healthy. The CLI dials the host-forwarded agent port directly (same pattern as `cp` / `fs`). Falls back to SSH when the agent is missing or unhealthy.

```bash
grain sh                         # auto-create if no VMs; agent PTY if up, else SSH
grain sh sbox-1
grain sh --agent sbox-1          # require agent PTY (error if unavailable)
grain sh --ssh sbox-1            # force classic SSH
```

Protocol: WebSocket binary frames carry PTY bytes both ways; optional text JSON control frames resize the PTY (`{"type":"resize","cols":N,"rows":M}`). The agent spawns a login shell as uid 1000 when present, otherwise root. Local stdin is put in raw mode when attached to a TTY; `SIGWINCH` is forwarded as resize frames.

### Copy

```bash
grain cp ./local.txt sbox-1:/tmp/local.txt
grain cp sbox-1:/var/log/cloud-init.log ./cloud-init.log
grain cp --agent ./a sbox-1:/tmp/a
grain cp --ssh   ./a sbox-1:/tmp/a
```

### Filesystem (agent only)

```bash
grain fs ls   sbox-1 /tmp
grain fs stat sbox-1 /etc/os-release
grain fs mkdir -p sbox-1 /tmp/a/b
grain fs rm -r sbox-1 /tmp/a
```

## Networking

Every VM gets a host-forwarded agent port (metadata `agent_port` → guest `7475`), same loopback-only pattern as SSH. List with:

```bash
grain fwd ls
```

### Agent transport (vsock vs TCP)

| Mode | When | How the host dials |
|------|------|--------------------|
| **vsock** | Linux host with `/dev/vhost-vsock`; config `agent_transport: auto` (default) or `vsock` | `AF_VSOCK` to guest CID **7475** (`agent_cid` on the instance) |
| **TCP hostfwd** | macOS HVF, no vhost device, `agent_transport: tcp`, or vsock dial failure | `http://127.0.0.1:<agent_port>` → guest `:7475` |

Config (`~/.grain/config.yaml`):

```yaml
agent_transport: auto  # auto | tcp | vsock
```

- **auto** — prefer vsock when `/dev/vhost-vsock` exists; otherwise TCP.  
- **tcp** — always SLIRP hostfwd (typical on macOS).  
- **vsock** — force QEMU `-device vhost-vsock-pci,guest-cid=<CID>` (requires the host device).

TCP hostfwd is **always** configured as a fallback even when vsock is selected. The guest agent always listens on TCP `:7475` and **additionally** tries AF_VSOCK port `7475` on Linux (listen failure is ignored — TCP-only is fine).

Host dial path (`agent.Dial`):

1. If `agent_cid > 0`, try vsock `CID:7475`  
2. Else (or on vsock failure) use `http://127.0.0.1:agent_port`

Instance metadata:

| Field | Meaning |
|-------|---------|
| `agent_port` | Host TCP port for SLIRP hostfwd |
| `agent_cid` | Guest vsock context ID (`0` / omitted = TCP only) |

## Soft-fail and fallbacks

- Missing agent binary → no deploy; SSH still works.  
- Unhealthy agent → `grain sh` / `grain x` / `grain cp` fall back to SSH/scp unless `--agent`.  
- `grain fs` requires a healthy agent (no SSH fallback).

## Go SDK and OpenAPI

Programmatic access uses the public client package and the daemon OpenAPI surface:

```go
import "github.com/cxdy/grain/client"

c, err := client.DialUnix(filepath.Join(home, ".grain", "grain.sock"))
// c.Exec, c.ExecStream, c.AgentHealth, c.PutFile, …
```

- Spec: [`api/openapi.yaml`](../api/openapi.yaml) (embedded in the binary for discovery)  
- Auth: set `api_token` in config or `GRAIN_TOKEN` — see README **API** / **Config**

## Related

- [images.md](/guides/images/) — golden agent-baked images  
- [recipes/coding-agent.md](/guides/recipes/coding-agent/) — mount repo, run agent, cp results  
- [recipes/ci-ephemeral.md](/guides/recipes/ci-ephemeral/) — create → x → rm  
- [troubleshooting.md](/guides/troubleshooting/) — doctor, logs, timeouts  
