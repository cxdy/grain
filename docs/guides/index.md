---
title: Guides
description: Problem-oriented how-tos for everyday use and operating grain in production-like setups.
---

How-to guides assume you already installed grain and created at least one sandbox. Each page solves a specific job.

## Everyday use

| Guide | When you need it |
|-------|------------------|
| [Images & golden boots]({{ '/guides/images/' | relative_url }}) | Pull, import, bake, choose ubuntu/alpine/golden |
| [Guest agent]({{ '/guides/agent/' | relative_url }}) | Exec, shell, cp, fs without living in SSH |
| [Networking & ports]({{ '/guides/networking/' | relative_url }}) | Publish ports, live forwards, SLIRP limits |
| [Mounts & shares]({{ '/guides/mounts/' | relative_url }}) | Share host directories into the guest |
| [Profiles & presets]({{ '/guides/profiles/' | relative_url }}) | Named defaults and docker / k3s / act presets |
| [Pause, suspend, restore]({{ '/guides/lifecycle/' | relative_url }}) | Free CPU or RAM while keeping work |

## Security & ops

| Guide | Audience |
|-------|----------|
| [Egress proxy]({{ '/guides/proxy/' | relative_url }}) | Admins locking down outbound HTTP(S) |
| [Remote sandbox host]({{ '/guides/remote-host/' | relative_url }}) | Team box: systemd, API token, SSH tunnels, SDKs |
| [Secrets]({{ '/guides/secrets/' | relative_url }}) | Host secrets and inject into VMs |
| [Firecracker]({{ '/guides/firecracker/' | relative_url }}) | Linux experimental backend |
| [Troubleshooting]({{ '/guides/troubleshooting/' | relative_url }}) | When boot, agent, or QEMU misbehaves |

## Recipes

| Recipe | Outcome |
|--------|---------|
| [Coding agent]({{ '/guides/recipes/coding-agent/' | relative_url }}) | Isolated agent with a mounted repo |
| [k3s]({{ '/guides/recipes/k3s/' | relative_url }}) | Single-node Kubernetes lab |
| [Docker socket]({{ '/guides/recipes/docker-socket/' | relative_url }}) | Docker in the VM, socket on the host |
| [CI ephemeral]({{ '/guides/recipes/ci-ephemeral/' | relative_url }}) | Create → test → destroy |
| [GitHub Actions (act)]({{ '/guides/recipes/act/' | relative_url }}) | Run `act` inside an isolated microVM |

If you are brand new, start with the [install tutorial]({{ '/get-started/install/' | relative_url }}) instead.
