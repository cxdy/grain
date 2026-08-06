# Grain Desktop

Optional **operator console** for [grain](https://grainvm.com) — a thin client of the existing daemon API (not a second engine). Built with **[Wails v2](https://wails.io)** (Go + OS webview).

| | |
|--|--|
| Product name | **Grain** (menu bar / window title) |
| Binary | `grain-desktop` (CLI-friendly name) |
| macOS app | `desktop/build/bin/Grain.app` after `just desktop-build` |
| Backend logic | `../internal/desktop` (unit-tested) |
| UI | `frontend/dist` — Field Manual tokens + `logo.png` |
| Platforms | macOS, Linux (WebKitGTK 4.1 on Linux) |

**Local dial:** Prefer `~/.grain/grain.sock`; if the socket is missing, fall back to loopback TCP from `api:` in config with `api_token` / `GRAIN_TOKEN` (same as the CLI).

## Features

| Area | Notes |
|------|--------|
| **Sandboxes** | List (search/sort/compact), multi-select bulk start/stop/rm with confirm + progress |
| **Create** | Cold · from template · **from warm pool** (prefers pool when ready &gt; 0; honest empty state) |
| **Warm pool** | Settings: template / size / running mode · Apply (restarts local daemon) · Fill; **More → Promote to golden + fill** |
| **Bulk start preflight** | Capacity check from active host `GET /info` caps (`max_vms` / CPU / memory) |
| **Inspector** | Overview (agent **checking…** honesty, metrics charts) · Shell · Logs |
| **Multi-Run** | Parallel `sh -c` on selected hosts; **re-run failed**; **copy all** |
| **Activity** | Daemon feed (CLI/MCP/API/Desktop) + source filter; toasts |
| **Images** | Catalog + pull progress |
| **Recipes** | Library, form builder, official catalog, deploy preflight |
| **MCP / Doctor / Settings** | Host switcher, Advanced YAML, connections |

Remote profiles never attempt to start a remote engine. Site guide: [Grain Desktop](https://grainvm.com/docs/main/guides/desktop/) (source: `docs/content/docs/main/guides/desktop.md`).

## Install / launch

```bash
# Installer (prefers Release asset when present)
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash -s -- --desktop

# From this repository
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails doctor
just desktop-test
just desktop-build
./bin/grain-desktop            # launcher → Grain.app on macOS
# not ./bin/Grain — collides with CLI on case-insensitive volumes
```

### Linux packages (Debian/Ubuntu)

```bash
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
just desktop-build   # -tags webkit2_41; no macOS codesign
./bin/grain-desktop-bin
grain up
```

If the window paints but **nothing is clickable**: rebuild after Linux binding fixes; start the daemon with `grain up`; check terminal for JS errors.

## Prerequisites

- Go 1.25+
- [Wails CLI](https://wails.io/docs/gettingstarted/installation)
- CGO + OS webview (Xcode CLT on macOS; WebKitGTK on Linux)
- System `grain` CLI on `PATH` for local daemon start

## Config

Desktop reads `~/.grain/config.yaml` (same file as the CLI/daemon):

```yaml
connections:
  - name: local
  - name: lab
    api: http://127.0.0.1:7474   # often via SSH -L
    token_env: GRAIN_TOKEN_LAB

desktop:
  default_connection: local
  start_local_daemon: true

warm_pool:
  template: golden
  size: 2
  running: false
```

## Architecture

```text
UI (webview)  →  Wails bindings (desktop/)  →  internal/desktop.Service
                                                      →  github.com/cxdy/grain/client
                                                      →  grain daemon API
```

Lifecycle and health always go through the public HTTP/unix client. Local daemon start spawns the system `grain` binary — the Desktop process does **not** embed `internal/daemon`.

## License

Apache-2.0 (same as grain).
