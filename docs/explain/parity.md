---
title: "Product surface"
description: "What grain implements for local Linux microVM sandboxes."
---

grain’s local microVM product surface as of the v0.2 line.

## Complete for local sandboxes

| Area | Status |
|------|--------|
| Daemon + unix/TCP API + optional auth | Done |
| Ephemeral / persistent lifecycle | Done |
| Guest agent (exec, shell PTY, cp, fs, stats, secrets) | Done |
| Create wait modes (auto/ssh/agent/userdata) | Done |
| Golden `grain-ubuntu` pull/bake/import | Done |
| Multi-distro: ubuntu-cloud, alpine-cloud | Done |
| Port forwards (create + live) | Done |
| Mounts 9p; virtiofs on Linux | Done |
| Pause/resume; suspend/restore | Done |
| Resource caps, profiles, presets | Done |
| Egress proxy (default-deny + secret inject) | Done |
| Go / TypeScript / Python SDKs + OpenAPI | Done |
| MCP server (`grain-mcp`) for agent hosts | Done |
| Install script, recipes, create bench | Done |
| Guest arch selection (`--arch`, incl. x86_64 on Apple Silicon via QEMU) | Done |
| Virtio GPU (`--gpu` / `gpu: virtio`) | Done |
| Shared overlay network between VMs (`network: overlay`) | Done |
| Firecracker backend | Experimental (Linux) |

**Platforms:** macOS and Linux hosts only. Windows / WSL are not supported (use the remote API/SDKs against a supported host).

## Quick verification

```bash
just test && just smoke-api
just build && just agent-linux
./bin/grain doctor
# live (optional):
# ./bin/grain up && ./bin/grain image pull grain-ubuntu && ./scripts/bench-create.sh
```

## Create latency

Measure on your hardware (numbers vary by image, wait mode, and host):

```bash
grain up
grain image pull grain-ubuntu
./scripts/bench-create.sh -n 5 --wait agent
```

Use the reported p50/p95 when talking about boot speed — do not invent sub-300ms claims without a local run.
