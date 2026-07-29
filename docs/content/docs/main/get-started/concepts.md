---
title: Core concepts
description: A short glossary of daemon, images, sandboxes, the guest agent, and how they fit together.
section: get-started
---

## Daemon

`grain up` starts a local control plane that owns VM metadata, disks, and the API (unix socket and optional TCP). CLI commands talk to this process — not directly to QEMU.

## Images vs VMs

| Term | Meaning |
|------|---------|
| **Base image** | Shared disk under `~/.grain/images/<id>/` (for example `grain-ubuntu`, `ubuntu-cloud`) |
| **VM / sandbox** | Named instance with its own overlay disk, seed, ports, and metadata under `~/.grain/vms/<name>/` |

Pull or import a base image once. Each `grain new` clones it (qcow2 overlay or CoW) so creates stay relatively cheap.

## Ephemeral vs persistent

- **Ephemeral (default):** stop or daemon restart removes the VM  
- **Persistent (`-p`):** disk kept; use `stop` / `start` or `suspend` / `restore`  

## Guest agent

A small HTTP service inside the Linux guest (`grain-agent`). The host reaches it via SLIRP hostfwd (and optionally vsock). It powers:

- `grain x` (exec)  
- `grain sh` (PTY shell)  
- `grain cp` / `grain fs`  
- health, stats, secret materialize  

SSH still works as a fallback and for bootstrap when the agent is not baked into the image.

## Control plane API

Automation can use:

- Unix socket: `~/.grain/grain.sock`  
- TCP: `http://127.0.0.1:7474` (default config)  
- OpenAPI: `GET /openapi.yaml`  
- SDKs: [Go](/docs/0.2.2/reference/go-sdk/) · [TypeScript](/docs/0.2.2/reference/typescript-sdk/) · [Python](/docs/0.2.2/reference/python-sdk/)  


Optional Bearer auth: set `api_token` in config and `GRAIN_TOKEN` in the environment.

For a **shared remote machine** (daemon as a service, teammates over SSH or SDKs), see [Remote sandbox host](/docs/0.2.2/guides/remote-host/). The CLI uses the **local unix socket** on the machine where it runs; remote automation uses the TCP API or an SSH session on the host.

## Hypervisors

| Value | Role |
|-------|------|
| `qemu` (default) | Production path on macOS and Linux |
| `mock` | Tests / CI without QEMU |
| `firecracker` | Experimental Linux-only backend |

## Where data lives

```text
~/.grain/
  config.yaml
  grain.sock
  ssh/           # host identity for guests
  images/        # base disks
  vms/           # per-VM disks, serial logs, QMP
  agent/         # grain-agent-linux-* for deploy
  secrets/       # host secret store
  proxy/         # egress proxy state
```
