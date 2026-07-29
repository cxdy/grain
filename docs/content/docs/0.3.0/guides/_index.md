---
title: Guides
description: Problem-oriented how-tos for everyday use and operating grain in production-like setups.
section: guides
---

How-to guides assume you already installed grain and created at least one sandbox. Each page solves a specific job.

If you are brand new, start with the [quick start](/docs/0.3.0/get-started/quickstart/).

## Popular workloads

| Guide | Outcome |
|-------|---------|
| [GitHub Actions (act)](/docs/0.3.0/guides/recipes/act/) | Run [nektos/act](https://github.com/nektos/act) in an isolated microVM — host Docker stays clean |
| [k3s lab](/docs/0.3.0/guides/recipes/k3s/) | Single-node Kubernetes with `--preset k3s`, API port + kubeconfig |

```bash
grain act -- -j test
grain new --preset k3s -n lab -p --wait userdata
```

## Everyday use

| Guide | When you need it |
|-------|------------------|
| [Images & golden boots](/docs/0.3.0/guides/images/) | Pull, import, bake, choose ubuntu/alpine/golden |
| [Guest agent](/docs/0.3.0/guides/agent/) | Exec, shell, cp, fs without living in SSH |
| [Networking & ports](/docs/0.3.0/guides/networking/) | Publish ports, live forwards, SLIRP limits |
| [Overlay network](/docs/0.3.0/guides/networking-overlay/) | Guest↔guest L2 on one host (`network: overlay`) |
| [Guest architecture](/docs/0.3.0/guides/multi-arch/) | arm64 / amd64 guests (TCG cross-arch) |
| [Virtio GPU](/docs/0.3.0/guides/gpu/) | `virtio-gpu-pci` for guests |
| [Mounts & shares](/docs/0.3.0/guides/mounts/) | Share host directories into the guest |
| [Profiles & presets](/docs/0.3.0/guides/profiles/) | Named defaults and docker / k3s / act presets |
| [Pause, suspend, restore](/docs/0.3.0/guides/lifecycle/) | Free CPU or RAM while keeping work |

## Security & ops

| Guide | Audience |
|-------|----------|
| [Egress proxy](/docs/0.3.0/guides/proxy/) | Admins locking down outbound HTTP(S) |
| [Remote sandbox host](/docs/0.3.0/guides/remote-host/) | Team box: systemd, API token, SSH tunnels, SDKs |
| [MCP server](/docs/0.3.0/guides/mcp/) | Claude Code / Codex / OpenCode / Grok Build tool host |
| [Secrets](/docs/0.3.0/guides/secrets/) | Host secrets and inject into VMs |
| [Firecracker](/docs/0.3.0/guides/firecracker/) | Linux experimental backend |
| [Troubleshooting](/docs/0.3.0/guides/troubleshooting/) | When boot, agent, or QEMU misbehaves |

## More recipes

| Recipe | Outcome |
|--------|---------|
| [Coding agent](/docs/0.3.0/guides/recipes/coding-agent/) | Isolated agent with a mounted repo |
| [Docker socket](/docs/0.3.0/guides/recipes/docker-socket/) | Docker in the VM, socket on the host |
| [CI ephemeral](/docs/0.3.0/guides/recipes/ci-ephemeral/) | Create → test → destroy |
