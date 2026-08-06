---
title: "Guides (how-tos by job)"
description: Problem-oriented how-tos for everyday use and operating grain in production-like setups.
section: guides
keywords:
  - guides
  - how-to
  - recipes
---

How-to guides assume you already installed grain and created at least one sandbox. Each page solves a specific job.

If you are brand new, start with the [quick start](../get-started/quickstart/).

## Popular workloads

| Guide | Outcome |
|-------|---------|
| [GitHub Actions (act)](./recipes/act/) | Run [nektos/act](https://github.com/nektos/act) in an isolated microVM — host Docker stays clean |
| [k3s lab](./recipes/k3s/) | Single-node Kubernetes with `--preset k3s`, API port + kubeconfig |

```bash
grain act -- -j test
grain new --preset k3s -n lab -p --wait userdata
```

## Everyday use

| Guide | When you need it |
|-------|------------------|
| [Grain Desktop (GUI)](./desktop/) | Optional operator console (sandboxes, shell, images, MCP, doctor) |
| [Images & golden boots](./images/) | Pull, import, bake, choose ubuntu/alpine/golden |
| [Guest agent](./agent/) | Exec, shell, cp, fs without living in SSH |
| [Networking & ports](./networking/) | Publish ports, live forwards, SLIRP limits |
| [Overlay network](./networking-overlay/) | Guest↔guest L2 on one host (`network: overlay`) |
| [Guest architecture](./multi-arch/) | arm64 / amd64 guests (TCG cross-arch) |
| [Virtio GPU](./gpu/) | `virtio-gpu-pci` for guests |
| [Mounts & shares](./mounts/) | Share host directories into the guest |
| [Profiles & presets](./profiles/) | Named defaults and docker / k3s / act presets |
| [Pause, suspend, restore](./lifecycle/) | Free CPU or RAM while keeping work |

## Security & ops

| Guide | Audience |
|-------|----------|
| [Remote lab happy path](./remote-lab/) | Host + laptop CLI: token, tunnel, `remote-coding`, sync, ports |
| [Remote sandbox host](./remote-host/) | Team box: systemd, firewall, reverse proxy, SDKs |
| [Egress proxy](./proxy/) | Admins locking down outbound HTTP(S) |
| [MCP server](../mcp/) | Claude Code / Codex / OpenCode / Grok Build tool host |
| [Secrets](./secrets/) | Host secrets and inject into VMs |
| [Firecracker](./firecracker/) | Supported Linux+KVM backend (agent + partial net) |
| [Troubleshooting](./troubleshooting/) | When boot, agent, or QEMU misbehaves |

## More recipes

| Recipe | Outcome |
|--------|---------|
| [Coding agent](./recipes/coding-agent/) | Isolated agent with a mounted repo |
| [Docker socket](./recipes/docker-socket/) | Docker in the VM, socket on the host |
| [CI ephemeral](./recipes/ci-ephemeral/) | Create → test → destroy |
