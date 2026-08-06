---
title: "Configuration reference (config.yaml)"
description: All knobs in ~/.grain/config.yaml for daemon and CLI defaults.
section: reference
keywords:
  - config
  - config.yaml
  - api_token
  - mcp
  - defaults
---

Default path: `~/.grain/config.yaml`. Override with `grain --config path …`.

## Core

```yaml
data_dir: ~/.grain           # created 0700 (owner-only); see SECURITY.md
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474          # daemon TCP listen (empty = unix only); cleartext HTTP
api_url: ""                  # CLI-only: remote base URL (http:// or https://; env GRAIN_API / --api)
api_token: ""                # or auth_token — Bearer when set; required for non-loopback api bind
# env GRAIN_TOKEN also accepted by CLI
# Remote team host: prefer api: 127.0.0.1:7474 + token + SSH tunnel (or TLS reverse proxy);
#   cleartext non-loopback http:// sniffs Bearer — CLI warns unless GRAIN_INSECURE_HTTP=1
# see guides/remote-lab and guides/remote-host
cpus: 2
memory_mb: 2048
disk_gb: 8
hypervisor: qemu             # qemu | mock | firecracker
qemu_binary: ""              # auto per arch
image: auto                  # auto | ubuntu-cloud | grain-ubuntu | alpine-cloud
ssh_user: ubuntu
ready_timeout: 2m
log_level: info
check_updates: true          # stderr note when a newer GitHub Release exists (CLI only)

mcp:
  enabled: false             # true = grain up starts MCP Streamable HTTP
  listen: 127.0.0.1:7476     # MCP endpoint http://LISTEN/mcp

warm_pool:
  template: ""               # persistent stopped/suspended golden (e.g. after grain suspend)
  size: 0                    # ready pre-clones to keep (0–32; 0 = disabled)
  running: false             # true = keep members agent-ready (uses host RAM)
```

Or one-shot: `grain up --mcp`. Stdio for IDE hosts: `grain mcp`. See [MCP server](../../mcp/).

**Warm pool:** set `template` + `size` &gt; 0, then `grain pool fill` (or restart the daemon — fill runs in the background). Claim with `grain new --from-pool` / `grain pool claim`. Default members are suspended/stopped clones (disk only). Set `running: true` to keep members agent-ready for rename-only claim. Desktop: **Settings → Warm pool** or **More → Promote to golden + fill**. See [lifecycle](../../guides/lifecycle/) and [Desktop](../../guides/desktop/).

`GET /info` exposes resource caps (`max_vms`, `max_cpus_total`, `max_memory_mb_total`, …) as strings so Desktop bulk-start preflight can hard-block over-cap batches on local and remote hosts.

Upgrade notices (not `grain update` itself) can also be disabled with env:

| Env | Effect |
|-----|--------|
| `GRAIN_CHECK_UPDATES=0` / `false` / `off` | Disable notices |
| `GRAIN_CHECK_UPDATES=1` / `true` / `on` | Force-enable notices |
| `GRAIN_NO_UPDATE_CHECK=1` | Disable notices |

Use `grain update --check` anytime; `grain update` installs the latest release via the public install script.

### Daemon listen vs CLI remote URL

| Field / env | Who uses it | Meaning |
|-------------|-------------|---------|
| `api` | **Daemon** | TCP **listen** address (`127.0.0.1:7474`, `0.0.0.0:7474`, …); cleartext HTTP |
| `api_url` / `GRAIN_API` / `--api` | **CLI client** | Base URL to dial (`http://127.0.0.1:7474` via tunnel, or `https://…` behind TLS proxy) |
| `api_token` / `GRAIN_TOKEN` | Both | Shared Bearer secret (sniffable on cleartext HTTP) |
| `GRAIN_INSECURE_HTTP=1` | **CLI client** | Silence one-time warning for non-loopback `http://` |

Non-loopback `api` without `api_token` → daemon **refuses to start**. Non-loopback `api_url` without a token → CLI errors. Prefer loopback + tunnel or HTTPS terminator for remote LAN use.

## Images & mounts

```yaml
mount_driver: 9p             # 9p | virtiofs (virtiofs Linux-only when virtiofsd exists)
agent_transport: auto        # auto | tcp | vsock
```

## Guest arch, GPU, network

```yaml
guest_arch: ""               # empty = host | arm64 | amd64 (x86_64 on Apple Silicon = QEMU TCG)
gpu: ""                      # empty | virtio (virtio-gpu-pci)
network: slirp               # slirp (isolated) | overlay (shared L2 between VMs on this host)
```

CLI: `grain new --arch amd64 --gpu virtio --network overlay`.

## Firecracker

```yaml
hypervisor: firecracker
firecracker_binary: firecracker   # PATH lookup or absolute path
kernel_path: ""                   # default ~/.grain/kernels/vmlinux (under data_dir)
```

| Key | Default | Notes |
|-----|---------|--------|
| `hypervisor` | `qemu` | `firecracker` selects the Linux+KVM Firecracker backend; restart the daemon after changing |
| `firecracker_binary` | `firecracker` | Binary name on `PATH` or absolute path |
| `kernel_path` | empty | Guest **vmlinux**; empty uses `data_dir/kernels/vmlinux` |

Linux + KVM only. Agent uses Firecracker vsock UDS; optional TAP + TCP proxy for `-P` / `grain fwd`. Prefer raw rootfs (`grain-ubuntu-fc`). Full operator path: [Firecracker on Linux](../../guides/firecracker/).

## Resource caps

Zero means unlimited for that field when explicitly set; defaults are finite.

```yaml
max_vms: 8
max_cpus_total: 16
max_memory_mb_total: 32768
max_cpus_per_vm: 8
max_memory_mb_per_vm: 16384
```

## Egress proxy

```yaml
proxy_listen: 0.0.0.0:3128   # guests reach host as 10.0.2.2:3128 via SLIRP
```

## Profiles

Builtin (no config required): **`remote-coding`** — `persistent: true`, 4 CPU / 8192 MiB / 32 GiB, image `grain-ubuntu`. Override by defining the same name under `profiles:`.

```yaml
profiles:
  agent:
    cpus: 4
    memory_mb: 4096
    image: grain-ubuntu
    mounts:
      - {host: ".", guest: "/work"}
    forwards:
      - {guest_port: 3000}
    preset: ""
  k3s-lab:
    cpus: 2
    memory_mb: 4096
    persistent: true
    preset: k3s
```

Resolve order for `grain new`: **explicit flags → profile (config, else builtin) → global defaults**.
