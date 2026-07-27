---
title: Quick start
description: Install grain, drop in a starter config, and open a shell in a Linux microVM.
---

From zero to a shell in a few minutes. For platforms and alternate install paths, see [Install]({{ '/get-started/install/' | relative_url }}). For a guided walkthrough with the interactive demo, see [Your first sandbox]({{ '/get-started/first-sandbox/' | relative_url }}).

## 1. Install

```bash
curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash

# macOS
brew install qemu

# Debian/Ubuntu
# sudo apt-get install -y qemu-system qemu-utils

grain doctor
```

**Platforms:** macOS and Linux (amd64 / arm64). **Not supported:** Windows or WSL.

## 2. Starter config (optional)

Defaults work with no file. To customize, create `~/.grain/config.yaml`:

```yaml
# ~/.grain/config.yaml
data_dir: ~/.grain
socket: ~/.grain/grain.sock
api: 127.0.0.1:7474

# Create defaults
image: grain-ubuntu   # after: grain image pull grain-ubuntu
cpus: 2
memory_mb: 2048
disk_gb: 8
ssh_user: ubuntu

# Soft caps (0 = unlimited for that field when set)
max_vms: 8
max_cpus_total: 16
max_memory_mb_total: 32768

# Named create profiles — grain new --profile work
profiles:
  work:
    cpus: 4
    memory_mb: 4096
    disk_gb: 20
    image: grain-ubuntu
    mounts:
      - {host: ".", guest: "/work"}
    forwards:
      - {guest_port: 3000}   # host port auto-assigned
```

Full field list: [Configuration reference]({{ '/reference/config/' | relative_url }}).

## 3. First sandbox

```bash
grain up
grain image pull grain-ubuntu   # once — golden image with guest agent
grain new                       # or: grain new --profile work
grain sh                        # name optional if only one VM
grain x -- uname -a
grain rm
grain down
```

| Command | What it does |
|---------|----------------|
| `grain up` / `down` | Start / stop the local daemon |
| `grain image pull` | Download a base image once |
| `grain new` | Create a microVM |
| `grain sh` / `x` | Shell / one-shot exec |
| `grain ls` / `rm` | List / delete |

## Next steps

- [Your first sandbox]({{ '/get-started/first-sandbox/' | relative_url }}) — demo + flags (`-p`, `-v`, `-P`)
- [Core concepts]({{ '/get-started/concepts/' | relative_url }}) — daemon, images, agent, API
- [CLI reference]({{ '/reference/cli/' | relative_url }}) · [HTTP API]({{ '/reference/api/' | relative_url }})
- SDKs: [Go]({{ '/reference/go-sdk/' | relative_url }}) · [TypeScript]({{ '/reference/typescript-sdk/' | relative_url }}) · [Python]({{ '/reference/python-sdk/' | relative_url }})
- Recipes: [coding agent]({{ '/guides/recipes/coding-agent/' | relative_url }}), [act]({{ '/guides/recipes/act/' | relative_url }}), [k3s]({{ '/guides/recipes/k3s/' | relative_url }})
