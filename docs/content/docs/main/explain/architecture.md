---
title: Architecture
description: How the grain daemon, hypervisor, guest agent, and CLI fit together.
section: explain
---

## Big picture

```text
CLI / SDKs
    │  unix socket or TCP (+ optional Bearer token)
    ▼
grain daemon
    │
    ├── store (VM meta under ~/.grain/vms)
    ├── image manager (~/.grain/images)
    ├── secrets / proxy state (optional features)
    │
    └── hypervisor runtime
            ├── qemu (default)  — HVF on Apple Silicon, KVM on Linux
            ├── firecracker     — experimental, Linux
            └── mock            — tests

Linux guest
    ├── cloud-init NoCloud seed (hostname, keys, mounts)
    ├── grain-agent :7475  (exec, shell, cp, fs, stats)
    └── optional sshd
```

## Control plane

The daemon is the source of truth for which VMs exist, their ports, and disks. The CLI is a thin client. Automation should prefer the HTTP API or SDKs so behavior stays consistent.

## Data path for a create

1. Resolve image (`auto` → golden if Ready, else ubuntu-cloud)  
2. Ensure base disk (pull/import)  
3. Clone overlay / CoW disk  
4. Write cloud-init seed ISO  
5. Start hypervisor with hostfwd for SSH and agent  
6. Wait for readiness (`ssh` / `agent` / `userdata`)  
7. Optionally deploy agent over SSH if not baked  

## Networking model (QEMU user mode)

Guests use SLIRP user networking:

- Host → guest services: **hostfwd** (`--publish`, SSH, agent port)  
- Guest → host: often **`10.0.2.2`** (egress proxy listens so guests can use it)  
- Guest ↔ guest: not supported without extra networking  

## Why a guest agent?

SSH is excellent for interactive login and bootstrap. The agent is better for:

- Structured exec with exit codes  
- Streaming output  
- File and filesystem operations without scp edge cases  
- Readiness probes (`/health`) independent of shell profiles  

See [Agent vs SSH](/docs/0.2.2/explain/agent-vs-ssh/).
