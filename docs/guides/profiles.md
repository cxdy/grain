---
title: "Profiles and presets"
description: "Named create defaults and docker/k3s presets."
---


## Named profiles

Define reusable create defaults in `~/.grain/config.yaml`:

```yaml
profiles:
  agent:
    cpus: 4
    memory_mb: 4096
    disk_gb: 20
    image: ubuntu-cloud
    persistent: false
    mounts:
      - {host: ".", guest: "/work"}
    forwards:
      - {guest_port: 3000}   # host port auto-allocated
  k3s-lab:
    cpus: 2
    memory_mb: 4096
    disk_gb: 20
    persistent: true
    preset: k3s
```

```bash
grain profile ls
grain new --profile agent
grain new --profile k3s-lab -n lab
```

**Resolve order:** CLI flags (only if explicitly set) → profile fields → global config defaults.

Created instances are tagged `profile=<name>` for inspect via the API / meta.

## Presets

Embedded cloud-init fragments applied with `grain new --preset NAME` or `profile.preset`:

| Preset | Effect |
|--------|--------|
| `docker` | Install Docker (cloud-init packages / install path) |
| `k3s` | Single-node k3s; defaults toward 2 CPU / 4096 MiB when unset; auto-publishes guest **6443** |

Presets merge into userdata with the structured cloud-init merge (safe with `--userdata-file`).

```bash
grain new --preset docker
grain new --preset k3s -n lab -p
# first boot can take several minutes — use:
grain logs -f lab
```

## Combining

```bash
grain new --profile agent --preset docker   # profile resources/mounts + docker userdata
grain new --profile agent -c 8              # flag overrides profile cpus
```
