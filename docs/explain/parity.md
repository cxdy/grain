---
title: "Product surface"
description: "What grain implements and what is intentionally deferred."
---


This documents grain’s local microVM product surface as of the Unreleased / v0.1 line. Competitor product names are intentionally omitted.

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
| Go SDK + TypeScript SDK + OpenAPI | Done |
| Install script, recipes, bench script | Done |
| Firecracker backend | Experimental (Linux) |

## Intentionally not built

| Area | Reason |
|------|--------|
| Menu bar tray | Polish; not required for CLI/API parity |
| Rosetta | Requires Apple Virtualization.framework path |
| GPU passthrough | Niche; QEMU-only stretch |
| Multi-node overlay networking | Single-node labs covered by presets |
| Sub-300ms marketing | Use `scripts/bench-create.sh` on real hardware |
| Windows / WSL host | Nested microVMs need real KVM/HVF; WSL2 nested virt is unreliable — remote API/SDK instead |

## Quick verification

```bash
just test && just smoke-api
just build && just agent-linux
./bin/grain doctor
# live (optional):
# ./bin/grain up && ./bin/grain image pull grain-ubuntu && ./scripts/bench-create.sh
```
