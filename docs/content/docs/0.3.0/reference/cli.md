---
title: CLI reference
description: Complete grain command reference for interactive use.
section: reference
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

See [Remote sandbox host](/docs/0.3.0/guides/remote-host/) for firewall and systemd.

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
| `grain sh [name]` | Shell (agent PTY preferred) |
| `grain x [name] -- cmd…` | Exec (streaming agent preferred) |
| `grain cp src dst` | Copy (`NAME:path` or host path) |
| `grain fs ls\|stat\|mkdir\|rm` | Guest filesystem helpers |
| `grain logs [name] [-f] [--qemu]` | Serial or QEMU logs |
| `grain stats [name]` | Guest resource stats |
| `grain fwd ls\|add\|rm` | Port forwards |
| `grain agent health [name]` | Agent health JSON (includes readiness when present) |
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
| `--wait` | `auto` (default), `ssh`, `agent`, `userdata`, `bootstrap` |
| `-P` / `--publish` | `HOST:GUEST` or `GUEST` (repeatable) |
| `-v` / `--volume` | `HOST:GUEST` share (repeatable) |
| `--publish-socket` | Host↔guest unix socket forward |
| `--profile` | Named profile |
| `--preset` | `docker`, `k3s`, or `act` |
| `--userdata-file` | Extra cloud-init / shell |
| `--proxy` | Inject `HTTPS_PROXY` for egress proxy |

Name is optional for `sh` / `rm` / `x` / `fs` / etc. when exactly one VM exists.

### `sh` / `x` / `cp` path selection

| Flag | Behavior |
|------|----------|
| (default) | Prefer guest agent when healthy |
| `--agent` | Agent only; error if unavailable |
| `--ssh` | Force SSH/scp |

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

Full guide: [Recipe: GitHub Actions (act)](/docs/0.3.0/guides/recipes/act/).

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
