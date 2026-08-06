---
title: "Install grain (CLI + QEMU)"
description: Install the grain CLI on macOS or Linux, verify dependencies, and prepare for your first VM.
section: get-started
keywords:
  - install
  - brew
  - qemu
  - grain doctor
  - upgrade
  - go install
  - release binary
---

{{< only-need href="get-started/quickstart/" >}}
Install + first sandbox in one page if you just want a working shell.
{{< /only-need >}}

This tutorial gets the **grain** binary onto your machine and checks that QEMU (or your chosen hypervisor) is ready. When you finish, you will be able to run `grain version` and `grain doctor`.

## Supported platforms

| Host | Architectures | Notes |
|------|---------------|--------|
| **macOS** | arm64 (Apple Silicon), amd64 (Intel) | Apple Silicon recommended; QEMU + HVF |
| **Linux** | amd64, arm64 | QEMU; **KVM** strongly recommended |

grain runs **Linux microVMs** with hardware virtualization (HVF on macOS, KVM on Linux when available). That is ordinary virtualization on a supported host — not nested virtualization. **Nested** virtualization only applies if the machine *running grain* is already a VM (for example grain inside another hypervisor) and the outer layer must expose hardware virt to that guest.

**Windows (native):** not supported as a grain host. From Windows, drive a remote grain daemon on macOS or Linux via the [HTTP API](../../reference/api/) or [SDKs](../../reference/go-sdk/).

**WSL / WSL2:** the environment reports as Linux, so the install script accepts it the same way as other Linux hosts. You still need **hardware virtualization available to that environment** (same requirement as bare-metal Linux). Running grain *inside* a VM without virt exposed is the nested case above — not a special WSL-only rule.

The install script only accepts `darwin` and `linux` (`uname`) and exits with `unsupported OS` otherwise.

## What you need

- A supported host (table above)
- Network access once (download CLI and base image)
- **QEMU** for real VMs (default backend)

Package managers (Homebrew formula, apt/rpm, etc.) are **planned** but not shipped yet — track progress on GitHub issues/releases. Today you install via the script, a GitHub Release binary, or Go.

## Option A — Install script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash

# Optional: also install Grain Desktop (GUI) when a release asset exists,
# or build from source if you run the script inside a checkout with Wails.
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash -s -- --desktop
```

The script:

1. Detects OS and architecture  
2. Downloads the latest release binary of `grain`  
3. Installs the guest agent binary under `~/.grain/agent/` when available  
4. Falls back to `go install` if no release asset exists  
5. With `--desktop`: prefers a GitHub Release Desktop asset; otherwise may build via `just desktop-build` in a checkout  

Default install locations: `/usr/local/bin` if writable, otherwise `~/.local/bin`. Override with `GRAIN_INSTALL_DIR`.

After install, the script asks whether to **enable MCP by default** (`mcp.enabled` in `~/.grain/config.yaml`). Non-interactive installs skip the prompt; use `GRAIN_ENABLE_MCP=1` or `0` to force yes/no. If you skip, you can enable later with `grain up --mcp` — see [MCP server](../../mcp/).

Desktop developer builds: [Grain Desktop](../../guides/desktop/) and `desktop/README.md` in the repo.

## Upgrade

```bash
grain update              # install latest if newer than current
grain update --check      # report only (exit 1 when an update is available)
```

`grain update` re-runs the same install script as first-time install (GitHub Release binary preferred). Occasional upgrade notices on other commands can be turned off with `check_updates: false` or `GRAIN_CHECK_UPDATES=0`.

### Install QEMU

**macOS**

```bash
brew install qemu
```

**Linux (Debian/Ubuntu)**

```bash
sudo apt-get update
sudo apt-get install -y qemu-system qemu-utils
```

**Linux (Fedora)**

```bash
sudo dnf install -y qemu-system-x86 qemu-img
```

## Option B — Release binary

1. Open [GitHub Releases](https://github.com/cxdy/grain/releases)  
2. Download `grain_<os>_<arch>` (for example `grain_darwin_arm64`)  
3. `chmod +x` and move it onto your `PATH`  
4. Optionally download `grain-agent-linux-<arch>` into `~/.grain/agent/`

## Option C — From source

```bash
go install github.com/cxdy/grain/cmd/grain@latest
```

Or from a checkout:

```bash
git clone https://github.com/cxdy/grain.git
cd grain
just test && just build && just agent-linux
./bin/grain version
```

## Verify

```bash
grain version
grain doctor
```

`grain doctor` checks the data directory, SSH key material, QEMU / `qemu-img`, ISO tools (for cloud-init seeds), and soft-warns if the Linux guest agent binary is missing.

You should see checks that look healthy for your platform. Soft warnings (for example missing agent binary before the first golden image) are OK for SSH-only workflows.

## Next

- [Quick start](../quickstart/) — starter config + first sandbox in one page  
- [First sandbox](../first-sandbox/) — guided tutorial + interactive demo
