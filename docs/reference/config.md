---
title: Configuration reference
description: All knobs in ~/.grain/config.yaml for daemon and CLI defaults.
---

Default path: `~/.grain/config.yaml`. Override with `grain --config path …`.

## Core

```yaml
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474          # TCP API + metrics; empty for unix-only
api_token: ""                # or auth_token — Bearer required when set
# env GRAIN_TOKEN also accepted by CLI
cpus: 2
memory_mb: 2048
disk_gb: 8
hypervisor: qemu             # qemu | mock | firecracker
qemu_binary: ""              # auto per arch
image: auto                  # auto | ubuntu-cloud | grain-ubuntu | alpine-cloud
ssh_user: ubuntu
ready_timeout: 2m
log_level: info
```

## Images & mounts

```yaml
mount_driver: 9p             # 9p | virtiofs (virtiofs Linux-only when virtiofsd exists)
agent_transport: auto        # auto | tcp | vsock
```

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
