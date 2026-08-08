---
title: "Grain Desktop (optional GUI)"
description: "Wails-based operator console — thin client of the grain daemon for macOS and Linux."
section: guides
keywords:
  - desktop
  - GUI
  - Wails
  - operator console
  - grain-desktop
---

**Grain Desktop** is an optional GUI for local (and remote) grain sandboxes. It is a **thin client** of the same control-plane API as the CLI and SDKs — not a second engine, and **not Electron**.

{{< only-need href="get-started/quickstart/" >}}
CLI-first path: install → `grain up` → `grain new` → `grain sh`.
{{< /only-need >}}

## What you get

| Area | Notes |
|------|--------|
| **Sandboxes** | List with search/sort/compact density, create, start/stop/remove, multi-select bulk actions, right-side inspector |
| **Inspector** | Overview (agent badge, metrics charts) · Shell · Logs — in-app shell plus **open in new window**; **More → Export as recipe…** / **Save as library recipe** / **Promote to golden + fill pool** |
| **Agent honesty** | While a golden is still booting, failed agent health shows **checking…** (not “not installed”) until success or a short grace after create |
| **Images** | Catalog ready/missing, pull with progress |
| **Recipes** | Local library (`~/.grain/recipes`), **New from form…**, import file/URL (preview→add), official catalog, YAML edit (valid-only save), **Deploy…** with preflight. Import never auto-creates a VM. |
| **MCP** | Status, enable in config, copy IDE snippets, ensure-running (local) |
| **Doctor** | Host tool + daemon checks with fix hints |
| **Settings** | Preferences, **Warm pool** (template / size / running mode + Fill), hosts, Advanced config.yaml (strict validate) |
| **Activity** | Toasts + drawer for **all clients** (CLI/MCP/API/Desktop via daemon `GET /activity`); optional **source filter**; persisted across daemon restarts |
| **Hosts** | Top-bar switcher; remote profiles never start a remote engine |
| **Fast create** | New: cold / from template / **from warm pool** (prefers pool when ready &gt; 0; honest empty/unconfigured copy) |
| **Bulk Start** | Confirm dialog + progress; **preflight** against active host caps from `GET /info` (blocks over-cap) |
| **Multi-Run** | Multi-select **Run…** — progressive results, **re-run failed**, **copy all** (stdout+stderr) |

## Install / launch

### From this repository (developers)

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@latest
wails doctor

# from repo root
just desktop-test
just desktop-build
./bin/grain-desktop            # launcher → Grain.app on macOS (not ./bin/Grain — collides with CLI on case-insensitive volumes)
open desktop/build/bin/Grain.app
```

Requires **CGO** and the OS webview (WKWebView on macOS, WebKitGTK on Linux). See [`desktop/README.md`](https://github.com/cxdy/grain/blob/main/desktop/README.md).

### Installer flag

```bash
# CLI + Desktop attempt
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash -s -- --desktop

# or from a checkout
./scripts/install.sh --desktop
```

From **v0.8.0** onward, GitHub Releases attach Desktop assets built by the **Release Desktop** workflow:

| Platform | Asset | Install location |
|----------|--------|------------------|
| macOS | `Grain_darwin_<arch>.app.tar.gz` | `~/Applications/Grain.app` (+ optional `grain-desktop` launcher) |
| Linux | `grain-desktop_linux_<arch>.tar.gz` | `~/.local/bin/grain-desktop` (or `GRAIN_INSTALL_DIR`) |

`install.sh --desktop` prefers those assets. If missing (or offline), it builds from source in a checkout with `just` + Wails; otherwise it prints build instructions (non-fatal when the CLI still installs).

### Linux

```bash
# WebKitGTK 4.1 (Ubuntu 22.04+ / 24.04 / 26.04 — no 4.0 package)
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
just desktop-build          # uses -tags webkit2_41; no codesign
./bin/grain-desktop-bin     # or ./bin/grain-desktop
grain up                    # daemon must be up for list/create
```

`just desktop-build` on Linux skips macOS codesign/`ditto`/`sips` and builds with `-tags webkit2_41 -nopackage`.

If the window paints but **nothing is clickable**: rebuild after the Linux fixes (splash no longer blocks the UI; Wails bindings resolve lazily). Run from a terminal and check for JS/console errors; start the daemon with `grain up` first.

## UI map

```
Host ▾ · health · Activity · Doctor · Docs
Sandboxes | Images | Recipes | Settings
[ list .................. ] [ inspector: Overview | Shell | Logs ]
```

- **Shell** focuses the in-app terminal; **⧉** opens a second Grain window (`--shell`). Keyboard input goes through live Wails `ShellWrite` bindings.
- **Start** and **Stop** never appear together.
- Sandbox list: search by name/image/status; sort; compact density; always scrollable.
- Header **Refresh** reloads the **current** view only.
- Click a toast to open **Activity** for that event.
- Theme toggle respects light/dark; native title bar and scrollbars follow the theme.

## Config

Desktop reads `~/.grain/config.yaml` (same file as the CLI). Optional keys:

```yaml
connections:
  - name: local
  - name: lab
    api: http://127.0.0.1:7474
    token_env: GRAIN_TOKEN_LAB

desktop:
  default_connection: local
  start_local_daemon: true
```

Settings → Advanced edits YAML with **strict unknown-key validation** and a trailing newline. Saving may restart the local daemon.

### Warm pool (Settings + New)

| Control | Action |
|---------|--------|
| **Settings → Warm pool** | Set `template`, desired `size` (0 disables), optional **running** mode → **Apply warm pool** (writes config + restarts local daemon) → **Fill pool** |
| **More → Promote to golden + fill pool** | Suspend if running, set template to that sandbox, fill |
| **New sandbox** | Prefers **From warm pool** when ready &gt; 0; empty/unconfigured copy stays honest (no silent cold while implying pool is ready) |
| **Bulk Start** | Preflight against active host caps from `GET /info` (blocks over `max_vms` / CPU / memory when known) |
| **Activity** | Filter by `desktop` / `cli` / `mcp` / `api` |

See [lifecycle — fast create](../lifecycle/#fast-create-spawn-and-warm-pool).

### Multi-host Run

Select two or more sandboxes → **Run…**. Results stream per host (hostname highlighted). After a run:

| Control | Behavior |
|---------|----------|
| **Re-run failed** | Re-executes only hosts that had an error or non-zero exit |
| **Copy all** | Clipboard export with per-host stdout/stderr blocks |

### Activity feed

- Merges **daemon** events (`GET /activity`, all clients) with local UI notes.
- Persisted on the daemon host at `data_dir/activity.json`.
- Filter dropdown: All · Desktop · CLI · MCP · API/SDK.
- Clients should send `X-Grain-Client: desktop|cli|mcp|sdk` (Desktop does this automatically).

### Metrics charts

When sandbox metrics are enabled (default), Overview shows guest history from the host-side ring. See [Metrics](../../reference/metrics/).

Sandbox **disk** increases run `qemu-img resize` when the sandbox is **stopped** (refuses while running). Grow the guest filesystem separately after resize.

## Security

Same single-operator model as the CLI: whoever holds the unix socket or API token has full control. Prefer `token_env`. Cleartext remote HTTP is warned.

## Relationship to CLI / MCP

| Surface | Role |
|---------|------|
| CLI | Primary automation and scripts |
| MCP | Coding-agent tools (`grain up --mcp` / `grain mcp`) |
| Desktop | Situational awareness + lifecycle + attach |

Headless installs stay CLI-only; Desktop is optional.
