---
title: "Product surface"
description: "What grain implements for local Linux microVM sandboxes."
section: explain
keywords:
  - parity
  - product surface
  - status
  - roadmap
  - features
  - experimental
---

{{< only-need href="get-started/quickstart/" >}}
Try the product first; this page is a capability checklist.
{{< /only-need >}}

What the local microVM product implements as of the v0.2 line.

## Done for local sandboxes

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
| MCP server (`grain up --mcp` / `grain mcp`) for agent hosts | Done |
| Install script, recipes, create bench | Done |
| Guest arch selection (`--arch`, incl. x86_64 on Apple Silicon via QEMU) | Done |
| Virtio GPU (`--gpu` / `gpu: virtio`) | Done |
| Shared overlay network between VMs (`network: overlay`) | Done |
| Firecracker backend | **Supported:** agent production (vFC-1) + partial net (vFC-2) TAP/TCP publish/fwd on Linux+KVM (`fc-latest` amd64/arm64); overlay/mounts/UDP still QEMU-only |

**Firecracker support policy**

| Tier | What you get |
|------|----------------|
| **FC agent production (vFC-1)** | Linux+KVM; pull `fc-kernel` + `grain-ubuntu-fc`; doctor; create `--wait agent`; `grain x` / agent `sh` / cp / sync / MCP guest tools over vsock UDS + `CONNECT` |
| **FC net (vFC-2 partial)** | TAP + `-P`/`grain fwd` (host TCP proxy); needs CAP_NET_ADMIN |
| **Not on FC today (use QEMU)** | Overlay L2, 9p/virtiofs mounts, SLIRP proxy, virtio GPU |

See [Firecracker on Linux](../../guides/firecracker/) and [Hypervisor matrix](../hypervisor-matrix/). Jailer and multi-host CNI stay deferred; single-tenant only.

**Platforms:** macOS and Linux hosts with hardware virtualization. Native Windows is not a host (use the remote API/SDKs against a supported host). WSL is Linux from grain’s point of view; virt must be available to that environment.

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

Quote the reported p50/p95 for boot speed. Do not claim sub-300ms without a local run.
