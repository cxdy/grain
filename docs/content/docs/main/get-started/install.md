---
title: "Install"
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
Install and first sandbox in one page if you want a working shell quickly.
{{< /only-need >}}

Get the **grain** binary on your machine and confirm QEMU (or your chosen hypervisor) is ready. When you are done you should be able to run `grain version` and `grain doctor`.

## Supported platforms

| Host | Architectures | Notes |
|------|---------------|--------|
| **macOS** | arm64 (Apple Silicon), amd64 (Intel) | Apple Silicon recommended; QEMU + HVF |
| **Linux** | amd64, arm64 | QEMU; **KVM** strongly recommended |

grain runs **Linux microVMs** with hardware virtualization (HVF on macOS, KVM on Linux when available). That is ordinary virtualization on a supported host — not nested virtualization. Nested virt only matters if the machine *running grain* is already a VM and the outer layer must expose hardware virt to that guest.

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
2. Downloads the latest release binary of `grain` (or the one named by `GRAIN_VERSION`)
3. **Verifies** the archive's SHA-256 against the release's `checksums.txt`
4. Installs the guest agent binary under `~/.grain/agent/` when available (also verified)
5. Falls back to `go install` if no release asset exists (Go's module checksum database applies; `checksums.txt` does not)
6. With `--desktop`: prefers a GitHub Release Desktop asset (verified); otherwise may build via `just desktop-build` in a checkout

Default install locations: `/usr/local/bin` if writable, otherwise `~/.local/bin`. Override with `GRAIN_INSTALL_DIR`.

After install, the script asks whether to **enable MCP by default** (`mcp.enabled` in `~/.grain/config.yaml`). Non-interactive installs skip the prompt; use `GRAIN_ENABLE_MCP=1` or `0` to force yes/no. If you skip, you can enable later with `grain up --mcp` — see [MCP server](../../mcp/).

### Integrity verification and pinning

Every downloaded archive (CLI, guest agent, Desktop) is hashed and compared with
the entry in the release's `checksums.txt`. The install **fails closed**: a
missing `checksums.txt`, a missing entry, or a mismatch aborts before anything is
extracted or installed.

| Variable | Effect |
|----------|--------|
| `GRAIN_VERSION` | Release to install, for example `v0.11.2` or `0.11.2`. Default: latest. Invalid values are rejected |
| `GRAIN_SHA256` | Expected SHA-256 (64 hex characters) of the **CLI** archive. Takes precedence over `checksums.txt` for that file; the agent and Desktop archives still use `checksums.txt` |
| `GRAIN_INSECURE_SKIP_VERIFY` | Set to `1` to skip verification and print a warning. Not recommended |
| `GRAIN_INSTALL_DIR` | Install directory |
| `GRAIN_ENABLE_MCP` | `1` or `0` to answer the MCP prompt non-interactively |
| `GRAIN_INSTALL_DESKTOP` | `1` or `true` to also install Desktop (same as `--desktop`) |

Example: a reproducible install pinned to a hash you reviewed.

```bash
GRAIN_VERSION=v0.11.2 \
GRAIN_SHA256=4c5baef458e23557b971f553ac6fe624bdf1d50411950db5c3492fcd6bdcb9e8 \
  bash install.sh        # hash of grain_linux_amd64.tar.gz in v0.11.2
```

> **Limit of this check.** `checksums.txt` lives on the same release as the archives. It catches corrupted
downloads and partial tampering, but anyone who can rewrite the whole release can
rewrite the checksums too. For strict environments, review `install.sh`, pin
`GRAIN_VERSION` and `GRAIN_SHA256` from your own reviewed source, or build from
source (Option C).

`GRAIN_VERSION` and `GRAIN_INSTALL_DIR` are inherited by `grain update`, which re-runs the script. Unset `GRAIN_VERSION` if you want `grain update` to move to the latest release.

Desktop developer builds: [Grain Desktop](../../guides/desktop/) and `desktop/README.md` in the repo.

## Upgrade

```bash
grain update              # install latest CLI + Desktop if newer than current
grain update --check      # report only (exit 1 when an update is available)
```

`grain update` re-runs the install script with `--desktop` (CLI and Grain Desktop release assets when available). Desktop install failure is non-fatal if the CLI updates. Occasional upgrade notices on other commands can be turned off with `check_updates: false` or `GRAIN_CHECK_UPDATES=0`.

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
3. Download `checksums.txt` from the same release and verify: `sha256sum -c --ignore-missing checksums.txt` (macOS: `shasum -a 256 -c`)
4. Extract, `chmod +x`, and move it onto your `PATH`
5. Optionally download `grain-agent-linux-<arch>.tar.gz` into place under `~/.grain/agent/` (verify it the same way)

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

Healthy checks for your platform are enough to continue. Soft warnings (for example missing agent binary before the first golden image) are fine for SSH-only workflows.

## Next

- [Quick start](../quickstart/) — starter config + first sandbox in one page
- [First sandbox](../first-sandbox/) — guided walkthrough + interactive demo
