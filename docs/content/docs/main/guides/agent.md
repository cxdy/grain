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

On **Firecracker** there is no SLIRP hostfwd: the guest still listens on AF_VSOCK port **7475**, and the host side is Firecracker’s vsock UDS (`fc-vsock.sock` + `CONNECT`). Optional host TCP proxy can expose guest ports (including agent `:7475`) after TAP is up. See [Firecracker on Linux](../firecracker/#networking-and-agent).

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
| **Deploy** | — | automatic SSH install; `grain agent deploy` · `POST /vms/{name}/agent/deploy` |

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

Copy from tools inside the guest (for example **Grok Build**) reaches your laptop in two hops:

| Direction | Mechanism |
|-----------|-----------|
| **Copy (guest → laptop)** | Guest `pbcopy`/`xclip`/`wl-copy` shims emit **OSC 52** on the TTY. `grain sh` (local or remote) intercepts OSC 52 on the **client** and writes the laptop clipboard. |
| **Paste (laptop → guest)** | Guest `pbpaste`/`wl-paste`/`xclip -o` call agent `GET /clipboard`. The agent asks the active `grain sh` client over the shell WebSocket; the client reads the laptop clipboard and returns it. Requires an interactive `grain sh` session (not bare `grain x`). |

Shims live on `PATH` at `/var/lib/grain/bin/` (installed when the agent starts).

| Env | Effect |
|-----|--------|
| `GRAIN_OSC52_CLIPBOARD=0` | Disable host CLI OSC 52 copy intercept (paste via `/clipboard` still works if the client can read the clipboard) |
| `GRAIN_OSC52_PASSTHROUGH=0` | After copying, do not re-emit OSC 52 to the terminal |

**If Grok (or similar) says clipboard unavailable or paste fails:**

1. Redeploy a current agent and open a **new** `grain sh`: `grain agent deploy NAME` (restarts the unit).
2. Inside the guest: `which pbcopy pbpaste` → `/var/lib/grain/bin/…`.
3. On the machine running `grain sh`, ensure system clipboard tools exist (`pbcopy` / `pbpaste` or `wl-copy` / `wl-paste` / `xclip`).
4. Paste needs an active interactive shell session; `grain x` has no clipboard bridge.
5. Terminal **selection** copy (mouse drag + Cmd/Ctrl+C) is the host terminal UI, not grain.

### Terminal identity (Shift+Enter / keyboard protocol)

`grain sh` (local and remote/`GRAIN_API`) forwards host terminal identity into the guest PTY so TUIs and multiplexers can match local keyboard behavior. Without this, the guest only sees a generic `TERM=xterm-256color` and modified keys (especially **Shift+Enter**) often collapse to plain Enter.

Forwarded when set on the client:

| Category | Variables |
|----------|-----------|
| Core | `TERM`, `TERM_PROGRAM`, `TERM_PROGRAM_VERSION`, `COLORTERM` |
| iTerm2 | `LC_TERMINAL`, `LC_TERMINAL_VERSION`, `TERM_FEATURES`, `TERM_SESSION_ID`, `ITERM_SESSION_ID`, `ITERM_PROFILE` |
| Kitty / WezTerm / Windows Terminal / VTE | `KITTY_WINDOW_ID`, `KITTY_PID`, `WEZTERM_PANE`, `WEZTERM_UNIX_SOCKET`, `WT_SESSION`, `VTE_VERSION` |
| Locale | `LANG`, `LC_ALL`, `LC_CTYPE` |

**Troubleshooting Shift+Enter / odd keys inside `grain sh`:**

1. On the client (where you run `grain sh`), check `echo $TERM_PROGRAM $LC_TERMINAL $TERM_FEATURES`.
2. Inside the guest session: `echo $TERM_PROGRAM $LC_TERMINAL $TERM_FEATURES` — should match the client after a **new** `grain sh` (not an old session).
3. Use a **CLI and guest agent** that include terminal-env forwarding (0.5.0+ for the first cut; later builds add iTerm/Kitty/WezTerm keys). Redeploy the agent after upgrading the host: `grain agent deploy NAME`.
4. Host terminal must actually emit a modified-Enter sequence (iTerm2, Ghostty, Kitty, WezTerm with Kitty keyboard enabled: e.g. WezTerm `enable_kitty_keyboard = true`).
5. Nested **tmux/screen**: enable extended keys in the multiplexer (tmux 3.2+: `set -s extended-keys on`) and ensure the outer terminal identity still reaches the app.

If Shift+Enter still fails, try **Alt+Enter** as a fallback.

Protocol: WebSocket binary frames carry PTY bytes both ways; optional text JSON control frames resize the PTY (`{"type":"resize","cols":N,"rows":M}`). The agent spawns a login shell as uid 1000 when present, otherwise root. Local stdin is put in raw mode when attached to a TTY; `SIGWINCH` is forwarded as resize frames.

**Disconnect / host daemon restart:** Restarting the grain daemon drops all remote and local `grain sh` WebSocket sessions. The client restores cooked mode and clears common private modes (alternate screen, mouse tracking, Kitty keyboard, …) so the terminal is usable again. If the screen is still garbled, run `reset`. Reconnect with `grain sh NAME` once the daemon is back.

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

After upgrading the **host** CLI, an older in-guest agent may lack new features (terminal env, readiness fields, …). Redeploy so the guest picks up the new binary:

```bash
just agent-linux                 # if bin/grain-agent-linux-$arch is missing on the daemon host
grain agent deploy sbox-1        # SCP + systemd enable (local or remote CLI)
grain agent health sbox-1        # confirm agent_version
```

With `GRAIN_API`, the CLI calls `POST /vms/{name}/agent/deploy` so SSH hostfwd runs on the sandbox host. The **agent binary must exist on the daemon host** (not the laptop). Or recreate the VM from a golden image with a baked agent.

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

### Trust model

The guest agent has **no auth token**. Security depends on reachability:

| Who | How they reach the agent | Protected by |
|-----|--------------------------|--------------|
| Local CLI on the grain host | `http://127.0.0.1:<agent_port>` hostfwd (or vsock) | Hostfwd binds **loopback only** |
| Remote CLI / SDKs | HTTP(S) to the **daemon** (`GRAIN_API` + `GRAIN_TOKEN`); daemon proxies exec/shell/cp/fs | Daemon API auth — not agent auth |
| Other VMs on `network: overlay` | Guest→guest TCP to peer `:7475` on the shared L2 | **Nothing** — peers can control each other |

Do not publish guest **7475** with `-P`, and do not expose hostfwd agent ports off loopback. On a shared host, prefer remote API access over tunneling raw agent ports. Overlay details: [Overlay network](../networking-overlay/#security-note). Broader model: [Security model](../../explain/security/#guest-agent-trust-model).

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
