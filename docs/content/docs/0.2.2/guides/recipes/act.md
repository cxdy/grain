---
title: GitHub Actions with act
description: Run nektos/act inside a grain microVM — isolated GitHub Actions without polluting host Docker.
section: guides
---

[nektos/act](https://github.com/nektos/act) runs GitHub Actions workflows on your machine using Docker. **grain act** boots an isolated Linux microVM with Docker + act, mounts your project, runs act, then tears the sandbox down.

## Why grain + act?

| Concern | Docker-only `act` on the host | `grain act` |
|---------|------------------------------|-------------|
| Isolation | Shares host Docker / network | Full Linux VM boundary |
| Host pollution | Images and containers on host | Ephemeral guest (default) |
| Reproducibility | Host OS dependent | Consistent Ubuntu guest |

## Prerequisites

```bash
grain up
grain image pull grain-ubuntu   # or ubuntu-cloud
# QEMU installed (grain doctor)
```

Your repo should contain `.github/workflows/*.yml`.

## Quick start

From the repository root:

```bash
# List workflows / jobs (default if no args)
grain act -- -l

# Run a job (first run pulls a ~500MB act runner image; non-interactive)
grain act -- -j test

# Specific workflow file
grain act -- -W .github/workflows/ci.yml

# Keep the sandbox after act exits (debug with grain sh)
grain act --keep -- -j build
```

**Always put act flags after `--`** so the grain CLI does not consume them.

grain seeds `~/.config/act/actrc` in the sandbox so act does not prompt for Large/Medium/Micro image size.

## Flags (grain)

| Flag | Default | Meaning |
|------|---------|---------|
| `--dir` | current directory | Host path mounted at `/work` |
| `--name` | `act-<dirname>` | Sandbox name |
| `--cpus` | 2 | vCPUs |
| `--mem` | 4096 | Memory MiB |
| `--image` | auto | Base image id |
| `--timeout` | 15m | Create + ready + act |
| `--keep` | false | Do not delete VM after act |

## What happens under the hood

1. `grain new` with preset **`act`** (Docker Engine + act install)  
2. Mount project → `/work`  
3. Wait for `docker info` and `act` on `PATH`  
4. `cd /work && act …` via guest agent  
5. Delete sandbox (unless `--keep`)  

First boot installs packages and act (needs network); later runs on a warm image are faster. Prefer a golden image with the agent baked in for snappier create.

**Guest agent:** `grain act` uses `--wait agent`. Without a golden image, the host deploys `grain-agent` over SSH — you need a linux agent binary on the host:

```bash
just agent-linux
# or install release agent under ~/.grain/agent/
```

If create hangs on “waiting agent” for a long time, check that binary exists (`grain doctor`) and that you are on a recent grain (WaitAgent must not burn the full timeout before SSH deploy).

## Manual equivalent

```bash
grain new --preset act -v "$PWD:/work" -n act-lab --wait agent
grain x act-lab -- bash -lc 'cd /work && act -l'
grain sh act-lab   # optional
grain rm act-lab
```

## Tips

- **Secrets:** do not bake CI secrets into the image. Prefer act’s secret flags, or [grain proxy](/docs/0.2.2/guides/proxy/) for HTTP APIs.  
- **Large workflows:** raise `--mem` / `--cpus` / `--timeout`.  
- **Debug:** `--keep` then `grain logs <name>` and `grain sh <name>`.  
- **act config:** commit a `.actrc` in the repo; it is visible at `/work`.  

## See also

- [Docker socket recipe](/docs/0.2.2/guides/recipes/docker-socket/)  
- [CI ephemeral recipe](/docs/0.2.2/guides/recipes/ci-ephemeral/)  
- [Guest agent](/docs/0.2.2/guides/agent/)  
