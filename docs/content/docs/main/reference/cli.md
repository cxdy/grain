---
title: "CLI reference (all grain commands)"
description: Complete grain command reference for interactive use.
section: reference
keywords:
  - CLI
  - commands
  - grain new
  - grain sh
  - grain up
---

Global flags:

| Flag / env | Meaning |
|------------|---------|
| `--config path` | Config file (default `~/.grain/config.yaml`) |
| `--api URL` | Remote daemon HTTP base (overrides `GRAIN_API` and config `api_url`) |
| `GRAIN_API` | Same as `--api` |
| `GRAIN_TOKEN` | Bearer token (or config `api_token`) |

Local default: unix socket. Remote example:

```bash
export GRAIN_API=http://127.0.0.1:7474   # after ssh -L …
export GRAIN_TOKEN=…
grain ls
grain --api http://sandbox:7474 ls
```

See [Remote lab happy path](../../guides/remote-lab/) for the host + laptop workflow, and [Remote sandbox host](../../guides/remote-host/) for firewall and systemd.

## Daemon

| Command | Description |
|---------|-------------|
| `grain up [--fg] [--mcp]` | Start daemon (background by default; `--mcp` also serves MCP Streamable HTTP) |
| `grain down` | Stop daemon via pidfile (cleans stale pid/socket) |
| `grain mcp [--http] [--listen addr]` | MCP tool server (stdio default; `--http` uses `mcp.listen`) |
| `grain update [--check] [--force]` | Check GitHub Releases and install the latest CLI (re-runs the install script) |
| `grain uninstall [--purge] [-y]` | Remove CLI binary; `--purge` also deletes the data directory |
| `grain doctor` | Dependency checks |
| `grain version` | Print version |

### `grain update`

| Flag | Meaning |
|------|---------|
| `--check` | Only compare current vs latest release (exit **1** if an update is available) |
| `--force` | Re-run the installer even when already on the latest release |

Most other commands may print a one-line stderr note when a newer release is known (cached for 24h). Disable notices with `check_updates: false` in config, `GRAIN_CHECK_UPDATES=0`, or `GRAIN_NO_UPDATE_CHECK=1`.

## Images

| Command | Description |
|---------|-------------|
| `grain image ls` | Catalog + local readiness |
| `grain image pull [id]` | Download base image (`ubuntu-cloud`, `grain-ubuntu`, `alpine-cloud`) |
| `grain image import <path> [--id grain-ubuntu]` | Register a local qcow2 |

## VMs

| Command | Description |
|---------|-------------|
| `grain new` | Create sandbox (see flags below) |
| `grain ls` | List VMs |
| `grain rm [name]` | Delete |
| `grain stop [name]` | Graceful stop (ephemeral deleted) |
| `grain start [name]` | Start stopped persistent VM |
| `grain pause` / `resume` | Freeze / unfreeze vCPUs |
| `grain suspend` / `restore` | Free RAM / bring back persistent VM |
| `grain clone <src> [dst]` | Offline clone of a stopped/suspended persistent disk |
| `grain sh [name]` | Shell (agent PTY preferred) |
| `grain x [name] -- cmd…` | Exec (streaming agent preferred) |
| `grain cp src dst` | Copy (`NAME:path` or host path); both directions |
| `grain sync push\|pull` | Incremental host↔guest directory sync (agent required) |
| `grain fs ls\|stat\|mkdir\|rm` | Guest filesystem helpers |
| `grain logs [name] [-f] [--qemu]` | Serial or QEMU logs |
| `grain stats [name]` | Guest resource stats |
| `grain fwd ls\|add\|rm\|tunnel` | Port forwards; `tunnel` prints `ssh -L` lines for host loopback |
| `grain agent health [name]` | Agent health JSON (includes readiness when present) |
| `grain agent deploy [name]` | Install/refresh guest agent over SSH (local or remote via daemon API) |
| `grain status [name]` | One-line VM + guest readiness |

### `grain new` flags

| Flag | Meaning |
|------|---------|
| `-p` / `--persist` | Keep disk after stop |
| `-n` / `--name` | Name |
| `-c` / `--cpus` | vCPUs |
| `-m` / `--mem` | Memory MiB |
| `-d` / `--disk` | Disk GiB |
| `-i` / `--image` | Image id |
| `--from TEMPLATE` | Fast spawn: clone a stopped/suspended persistent template and start (`-loadvm` when snapshotted) |
| `--from-pool` | Claim a pre-cloned [warm pool](#warm-pool-grain-pool) member and start (mutually exclusive with `--from`) |
| `--wait` | `auto` (default), `ssh`, `agent`, `userdata`, `bootstrap` |
| `-P` / `--publish` | `HOST:GUEST` or `GUEST` (repeatable) |
| `-v` / `--volume` | `HOST:GUEST` share (repeatable) |
| `--publish-socket` | Host↔guest unix socket forward |
| `--profile` | Named profile from config, or builtin `remote-coding` |
| `--preset` | `docker`, `k3s`, or `act` |
| `--recipe` | Sandbox recipe YAML or library name (create + optional bootstrap steps) |
| `--userdata-file` | Extra cloud-init / shell (not with `--recipe`) |
| `--proxy` | Inject `HTTPS_PROXY` for egress proxy |

**Create path notes**

- Cold boot (`grain new` without `--from` / `--from-pool`): host work is ~hundreds of ms; remaining time is guest UEFI/kernel/agent (~seconds on `grain-ubuntu`). Daemon logs `create timing` (image/disk/seed/start/wait ms).
- Agent-ready images use a **minimal** cloud-init seed; disk growpart is **deferred** after agent when the clone disk is larger than the base image.
- `--from` / `--from-pool` skip a full cold boot when the template was `grain suspend`’d with a qcow2 snapshot. Guide: [lifecycle — fast create](../../guides/lifecycle/#fast-create-spawn-and-warm-pool).

### Warm pool (`grain pool`)

Pre-clone suspended (or optionally **running**) template VMs for fast claim. Config:

```yaml
warm_pool:
  template: golden   # persistent stopped/suspended VM name
  size: 2            # ready members to keep (0 disables; max 32)
  running: false     # true = keep members agent-ready (uses host RAM)
```

| Command | Description |
|---------|-------------|
| `grain pool status` | Ready count, desired size, members, running mode |
| `grain pool fill` | Clone template until ready == size (starts members if `running: true`) |
| `grain pool claim [-n NAME]` | Claim one member, rename, start (or rename-only if already running) |
| `grain pool drain` | Delete all ready pool members |

```bash
grain new -i grain-ubuntu -n golden -p --wait agent
grain suspend golden
# set warm_pool in config, then:
grain pool fill
grain new --from-pool -n work1
grain pool status
```

Desktop: **Settings → Warm pool** and **More → Promote to golden + fill pool**. See [Desktop](../../guides/desktop/) and [config](../config/).

### `grain recipe`

| Command | Meaning |
|---------|---------|
| `grain recipe list` | Recipes in `~/.grain/recipes` |
| `grain recipe add <file\|url\|id>` | Install into the library (never creates a VM) |
| `grain recipe search` | Official catalog index |
| `grain recipe preview <url\|id>` | Validate + summary without install |
| `grain recipe delete <id>` | Remove library file only |
| `grain recipe validate <file\|id>` | Schema + compile check |
| `grain recipe show <file\|id>` | Print resolved create options |
| `grain recipe show … --userdata` | Also print compiled cloud-init |

Guide: [Sandbox recipes](../../get-started/recipe/).

Name is optional for `sh` / `rm` / `x` / `fs` / etc. when exactly one VM exists.

### `sh` / `x` / `cp` path selection

| Flag | Behavior |
|------|----------|
| (default) | Prefer guest agent when healthy |
| `--agent` | Agent only; error if unavailable |
| `--ssh` | Force SSH/scp |

### `grain cp` (both directions)

```bash
# host → guest
grain cp ./script.sh lab:/tmp/script.sh
grain cp ~/proj lab:/work/proj/

# guest → host (reverse copy — same command, swap args)
grain cp lab:/work/proj/out.json ./out.json
grain cp lab:/var/log/cloud-init.log ./cloud-init.log
```

Remote CLI (`GRAIN_API`) uses the daemon’s agent proxy for `cp` — no scp from the laptop. Guest↔guest in one step is not supported (pull then push).

### `grain sync push | pull`

Incremental **directory** sync with a host-side baseline under `~/.grain/sync/` (or `data_dir/sync/`). Requires the guest agent (no scp fallback). Directory roots only — use `cp` for single files. Regular **symlinks** are transferred as links (target string; not followed); directory symlinks are not descended.

```bash
grain sync push  ~/proj  lab:/work/proj
grain sync pull  lab:/work/proj  ~/proj
grain sync push  ~/proj  lab:/work/proj --dry-run
grain sync pull  lab:/work/proj  ~/proj --delete --force
```

| Flag | Meaning |
|------|---------|
| `--delete` | Remove dest paths missing on source (ignored paths never deleted) |
| `--dry-run` | Plan only; no writes or state update |
| `--force` | Source-wins for conflicts and dest-ahead paths |
| `--exclude` | Extra gitignore-style patterns (repeatable) |
| `--no-defaults` | Skip built-in ignores (`.git/`) |
| `--no-gitignore` / `--no-grainignore` | Skip host ignore files |
| `--verbose` / `-v` | List skipped / kept_dest paths |
| `--max-file-size` | Skip source files larger than N bytes |

Exit codes: `0` ok, `1` usage, `2` conflicts (zero applies), `3` apply error. Dest-ahead paths are **kept** unless `--force` — successful sync is not always a full mirror.

MCP: `grain_sync_push` / `grain_sync_pull` with the same semantics.

## GitHub Actions (`grain act`)

Runs [nektos/act](https://github.com/nektos/act) inside an ephemeral sandbox (Docker + act preset). Put act flags after `--`.

```bash
grain act -- -l
grain act -- -j test
grain act --keep -- -W .github/workflows/ci.yml
```

| Flag | Default | Meaning |
|------|---------|---------|
| `--dir` | `.` | Host project mounted at `/work` |
| `--name` | `act-<dirname>` | Sandbox name |
| `--cpus` | 2 | vCPUs |
| `--mem` | 4096 | Memory MiB |
| `--image` | auto | Base image id |
| `--timeout` | 15m | Create + ready + act |
| `--keep` | false | Keep VM after act exits |

Full guide: [Recipe: GitHub Actions (act)](../../guides/recipes/act/).

## Secrets & proxy

| Command | Description |
|---------|-------------|
| `grain secret ls\|set\|rm\|inject` | Host secrets store |
| `grain proxy up\|down\|allow\|deny\|ls\|client` | Egress proxy process |

## Profiles

```bash
grain profile ls
grain new --profile agent
```
