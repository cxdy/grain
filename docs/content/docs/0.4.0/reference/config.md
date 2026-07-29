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
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474          # daemon TCP listen (empty = unix only)
api_url: ""                  # CLI-only: remote daemon base URL (or env GRAIN_API / --api)
api_token: ""                # or auth_token — Bearer when set; required for non-loopback api bind
# env GRAIN_TOKEN also accepted by CLI
# Remote team host: prefer api: 127.0.0.1:7474 + token + SSH tunnel; see guides/remote-host
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
```

Or one-shot: `grain up --mcp`. Stdio for IDE hosts: `grain mcp`. See [MCP server](../../mcp/).

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
| `api` | **Daemon** | TCP **listen** address (`127.0.0.1:7474`, `0.0.0.0:7474`, …) |
| `api_url` / `GRAIN_API` / `--api` | **CLI client** | Base URL to dial (`http://host:7474`) |
| `api_token` / `GRAIN_TOKEN` | Both | Shared Bearer secret |

Non-loopback `api` without `api_token` → daemon **refuses to start**. Non-loopback `api_url` without a token → CLI errors.

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

## Firecracker (experimental)

```yaml
hypervisor: firecracker
firecracker_binary: firecracker
kernel_path: ""              # default ~/.grain/kernels/vmlinux
```

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

Resolve order for `grain new`: **explicit flags → profile → global defaults**.
