---
title: Install grain
description: Install the grain CLI on macOS or Linux, verify dependencies, and prepare for your first VM.
---

This tutorial gets the **grain** binary onto your machine and checks that QEMU (or your chosen hypervisor) is ready. When you finish, you will be able to run `grain version` and `grain doctor`.

## Supported platforms

| Host | Architectures | Notes |
|------|---------------|--------|
| **macOS** | arm64 (Apple Silicon), amd64 (Intel) | Apple Silicon recommended; QEMU + HVF |
| **Linux** | amd64, arm64 | QEMU; **KVM** strongly recommended |

**Not supported:** Windows, **WSL** / WSL2, and other non-`darwin`/`linux` hosts. grain runs nested Linux microVMs (QEMU); WSL2 is already a VM and nested virtualization is unreliable for this use case. From Windows, drive a remote grain daemon via the [HTTP API]({{ '/reference/api/' | relative_url }}) or [SDKs]({{ '/reference/go-sdk/' | relative_url }}) instead.

The install script only accepts `darwin` and `linux` and will exit with `unsupported OS` otherwise.

## What you need

- A supported host (table above)
- Network access once (download CLI and base image)
- **QEMU** for real VMs (default backend)

Package managers (Homebrew formula, apt/rpm, etc.) are **planned** but not shipped yet — track progress on GitHub issues/releases. Today you install via the script, a GitHub Release binary, or Go.

## Option A — Install script (recommended)

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
```

The script:

1. Detects OS and architecture  
2. Downloads the latest release binary of `grain`  
3. Installs the guest agent binary under `~/.grain/agent/` when available  
4. Falls back to `go install` if no release asset exists  

Default install locations: `/usr/local/bin` if writable, otherwise `~/.local/bin`. Override with `GRAIN_INSTALL_DIR`.

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

- [Quick start]({{ '/get-started/quickstart/' | relative_url }}) — starter config + first sandbox in one page  
- [Your first sandbox]({{ '/get-started/first-sandbox/' | relative_url }}) — guided tutorial + interactive demo
