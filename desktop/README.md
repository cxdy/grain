# Grain Desktop

Optional **operator console** for [grain](https://grainvm.com) — a thin client of the existing daemon API (not a second engine). Built with **[Wails v2](https://wails.io)** (Go + OS webview).

| | |
|--|--|
| Product name | **Grain** (menu bar / window title) |
| Binary | `grain-desktop` (CLI-friendly name) |
| macOS app | `desktop/build/bin/Grain.app` after `just desktop-build` |
| Backend logic | `../internal/desktop` (unit-tested, ≥75% coverage) |
| UI | `frontend/dist` — Field Manual tokens + `logo.png` |
| Platforms | macOS, Linux (WebKitGTK on Linux) |

**Local dial:** Prefer `~/.grain/grain.sock`; if the socket is missing, fall back to loopback TCP from `api:` in config with `api_token` / `GRAIN_TOKEN` (same as the CLI).

## Features (ship-ready surface)

- **Sandboxes · Images · MCP · Doctor · Settings** sidebar
- Host switcher, health pill, activity drawer + toasts
- Right inspector: Overview · Shell · Logs; open-in-new-window shell
- Splash auto-start of the **local** daemon when unhealthy
- Strict Advanced config.yaml edit; disk grow via `qemu-img`
- Install: `./scripts/install.sh --desktop` or `just desktop-build`

Remote profiles never attempt to start a remote engine. See [docs guide](../docs/content/docs/main/guides/desktop.md).

## Prerequisites

- Go 1.25+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation):  
  `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
- CGO + OS webview (Xcode CLT on macOS; WebKitGTK on Linux)
- System `grain` CLI on `PATH` for local daemon start

```bash
wails doctor   # verify toolchain
```

### Linux packages (example Debian/Ubuntu)

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
# package names vary by distro; see wails doctor
```

## Develop

From the **repo root**:

```bash
just desktop-test    # pure Go tests (no webview)
just desktop-dev     # wails dev
just desktop-build   # bin/grain-desktop
```

Or from this directory:

```bash
cd desktop
wails dev
wails build -skipbindings -nopackage
```

This directory is a **nested Go module** (`github.com/cxdy/grain/desktop`) so the root `CGO_ENABLED=0` CLI CI path stays clean. It `replace`s `github.com/cxdy/grain => ../`.

## Config

Desktop reads `~/.grain/config.yaml` (same file as the CLI/daemon). Optional keys:

```yaml
connections:
  - name: local
  - name: lab
    api: http://127.0.0.1:7474   # often via SSH -L
    token_env: GRAIN_TOKEN_LAB
    notes: |
      ssh -N -L 7474:127.0.0.1:7474 user@lab

desktop:
  default_connection: local
  start_local_daemon: true
```

See design notes: `~/grain-notes/desktop-app/` (if present) and site docs under get-started.

## Architecture

```text
UI (webview)  →  Wails bindings (desktop/)  →  internal/desktop.Service
                                                      →  github.com/cxdy/grain/client
                                                      →  grain daemon API
```

Lifecycle and health always go through the public HTTP/unix client. Local daemon start spawns the system `grain` binary — the Desktop process does **not** embed `internal/daemon`.

## License

Apache-2.0 (same as grain).
