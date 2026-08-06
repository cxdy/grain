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

**Grain Desktop** is an optional GUI for managing local (and remote) grain sandboxes. It is a **thin client** of the same control-plane API as the CLI and SDKs — not a second engine, and **not Electron**.

{{< only-need href="get-started/quickstart/" >}}
CLI-first path: install → `grain up` → `grain new` → `grain sh`.
{{< /only-need >}}

## What you get

| Area | Notes |
|------|--------|
| **Sandboxes** | List, create, start/stop/remove, multi-select bulk actions, right-side inspector |
| **Inspector** | Overview · Shell · Logs — in-app shell plus **open in new window**; **More → Export as recipe…** writes a portable [`grain/v1` Sandbox recipe](../get-started/recipe/) (create options, mounts, forwards — not bootstrap/userdata) |
| **Images** | Catalog ready/missing, pull with progress |
| **Recipes** | Local library (`~/.grain/recipes`), **New from form…**, import file/URL (URL is preview→add), browse official catalog, YAML edit (valid-only save), **Deploy…** with preflight (image ready / mounts / remote host) + name override + wait. **More → Save as library recipe** from a sandbox. Import never auto-creates a VM. |
| **MCP** | Status, enable in config, copy IDE snippets, ensure-running (local) |
| **Doctor** | Host tool + daemon checks with fix hints |
| **Settings** | Preferences summary, hosts, Advanced config.yaml (strict validate) |
| **Activity** | Toasts + activity drawer (timings, errors) |
| **Hosts** | Top-bar switcher; remote profiles never start a remote daemon |

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

When a Desktop release artifact is not published yet, `--desktop` builds from source if you run the installer inside a grain checkout with `just` + Wails available; otherwise it prints build instructions (non-fatal if the CLI installed).

### Linux

```bash
# WebKitGTK 4.1 (Ubuntu 22.04+ / 24.04 / 26.04 — no 4.0 package)
sudo apt-get install -y libgtk-3-dev libwebkit2gtk-4.1-dev
just desktop-build          # uses -tags webkit2_41; no codesign
./bin/grain-desktop-bin     # or ./bin/grain-desktop
grain up                    # daemon must be up for list/create
```

`just desktop-build` on Linux skips macOS codesign/`ditto`/`sips` and builds with `-tags webkit2_41 -nopackage`.

If the window paints but **nothing is clickable**: ensure you rebuilt after the Linux fixes (splash no longer blocks the UI; Wails bindings resolve lazily). Also run from a terminal and check for JS/console errors; start the daemon with `grain up` first.

## UI map

```
Host ▾ · health · Activity · Doctor · Docs
Sandboxes | Images | Recipes | Settings
[ list .................. ] [ inspector: Overview | Shell | Logs ]
```

- **Shell** focuses the in-app terminal; **⧉** opens a second Grain window (`--shell`).
- **Start** and **Stop** never appear together.
- Header **Refresh** reloads the **current** view only.
- Click a toast to open **Activity** for that event.

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
