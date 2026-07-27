---
title: Menu bar tray
description: Run grain tray for a small macOS menu bar or Linux system tray helper.
---

`grain tray` adds a lightweight menu bar (macOS) or system tray (Linux) control for the local daemon. It does **not** replace the CLI — it surfaces status and docs shortcuts while you work.

## Prerequisites

- macOS or Linux (not Windows)
- A **CGO-enabled** grain binary (`grain tray` is stubbed out when `CGO_ENABLED=0`, including GitHub Release archives)
- `grain up` so the daemon is running (the tray still starts if the daemon is down; it shows **off**)

```bash
# local tray-capable build
CGO_ENABLED=1 just build-tray   # or: CGO_ENABLED=1 go build -o bin/grain ./cmd/grain
```

## Usage

```bash
grain up
grain tray
```

The title shows roughly `grain · N` where **N** is the count of active VMs (`running` / `creating` / `paused`). The menu includes:

- Live status line  
- **Open docs** → grainvm.com  
- **Quick start**  
- **Quit tray** (leaves the daemon running)

## Notes

- Build tags: tray uses [fyne.io/systray](https://github.com/fyne-io/systray) (CGO on some platforms).
- The tray is optional polish; all lifecycle remains available via CLI and API.
