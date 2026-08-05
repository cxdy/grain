---
title: "Guest agent (exec, shell, files, health)"
description: "Use grain-agent inside the VM for exec, shell, copy, fs, health, and readiness — without living in SSH."
section: guides
keywords:
  - agent
  - grain-agent
  - exec
  - shell
  - health
  - readiness
---

{{< only-need href="get-started/first-sandbox/" >}}
Open a shell and run commands — agent is used automatically when healthy.
{{< /only-need >}}

{{< only-need href="get-started/bootstrap/" >}}
Block create until custom setup finishes (readiness / `--wait bootstrap`).
{{< /only-need >}}

The guest agent is a small HTTP server that runs **inside** each Linux VM. The host CLI and daemon talk to it over either **virtio-vsock** (when the host supports it) or a QEMU SLIRP hostfwd to guest port **7475**, so common operations work without opening an interactive SSH session.

## What it provides

| Capability | Guest HTTP | Host CLI / API |
|------------|------------|----------------|
| **Health** | `GET /health` | `grain agent health [name]` · `GET /vms/{name}/agent/health` |
| **Readiness** | `GET /readiness` · fields on `/health` | `grain status [name]` · bootstrap wait — see [Readiness protocol](../../explain/readiness/) |
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
| `--wait bootstrap` | agent healthy, then readiness protocol `state=ready` (see [Readiness protocol](../../explain/readiness/)) |

Golden images (`grain-ubuntu` via `grain image import` / bake) set `has_agent` so create prefers probing the agent before SSH deploy. See [Images](../images/).

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

### Clipboard (OSC 52)

TUIs inside the guest (for example **Grok Build**) often copy via **OSC 52** escape sequences so the selection can reach the host over a PTY. `grain sh` intercepts those sequences on the **host CLI** and writes the payload to the local clipboard (`pbcopy` on macOS; `wl-copy` / `xclip` / `xsel` on Linux), including over remote `GRAIN_API` shells.

| Env | Effect |
|-----|--------|
| `GRAIN_OSC52_CLIPBOARD=0` | Disable host clipboard intercept |
| `GRAIN_OSC52_PASSTHROUGH=0` | After copying, do not re-emit OSC 52 to the terminal |

Sequences are still passed through by default so terminals with native OSC 52 (iTerm2, Ghostty, Kitty, …) keep working.

### Terminal identity (Shift+Enter / keyboard protocol)

`grain sh` forwards the host’s `TERM`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `COLORTERM`, and locale variables into the guest shell so tools like Grok Build can negotiate the same keyboard protocol as a local session (for example **Shift+Enter** for a newline). Without this, the guest only sees a generic `TERM=xterm-256color` and modified Enter keys often collapse to plain Enter.

If Shift+Enter still fails, try **Alt+Enter**, or enable Kitty keyboard support in your host terminal (WezTerm: `enable_kitty_keyboard = true`).

Protocol: WebSocket binary frames carry PTY bytes both ways; optional text JSON control frames resize the PTY (`{"type":"resize","cols":N,"rows":M}`). The agent spawns a login shell as uid 1000 when present, otherwise root. Local stdin is put in raw mode when attached to a TTY; `SIGWINCH` is forwarded as resize frames.

### Copy

```bash
# host → guest
grain cp ./local.txt sbox-1:/tmp/local.txt
# guest → host (reverse)
grain cp sbox-1:/var/log/cloud-init.log ./cloud-init.log
grain cp --agent ./a sbox-1:/tmp/a
grain cp --ssh   ./a sbox-1:/tmp/a
```

### Incremental sync

For iterative edit loops (seed → work in guest → bring results home), prefer `grain sync` over full-tree `cp`:

```bash
grain sync push ~/proj sbox-1:/work/proj
grain sh sbox-1
grain sync pull sbox-1:/work/proj ~/proj
```

Requires a healthy agent. See [CLI reference](../../reference/cli/#grain-sync-push--pull).

### Refresh / redeploy the guest agent

After upgrading the **host** CLI, an older in-guest agent may lack new features (terminal env, readiness fields, …). Redeploy from the machine that runs the daemon:

```bash
just agent-linux                 # if bin/grain-agent-linux-$arch is missing
grain agent deploy sbox-1        # SCP + systemd enable (local daemon only)
grain agent health sbox-1        # confirm agent_version
```

Remote CLI (`GRAIN_API`) cannot deploy over SSH hostfwd (ports live on the sandbox host). SSH to the host and run `grain agent deploy`, or recreate the VM from a golden image with a baked agent.

`grain doctor` reports whether the host-side agent binary is present (soft warning if missing).

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
- `grain fs` and `grain sync` require a healthy agent (no SSH fallback).

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

- [Images](../images/) — golden agent-baked images  
- [Coding agent recipe](../recipes/coding-agent/) — mount repo, run agent, cp results  
- [CI ephemeral recipe](../recipes/ci-ephemeral/) — create → x → rm  
- [Troubleshooting](../troubleshooting/) — doctor, logs, timeouts  
