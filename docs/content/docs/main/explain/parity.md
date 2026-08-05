---
title: "Product surface (what’s done / experimental)"
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
Use the product first — this page is a capability checklist.
{{< /only-need >}}

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
| MCP server (`grain up --mcp` / `grain mcp`) for agent hosts | Done |
| Install script, recipes, create bench | Done |
| Guest arch selection (`--arch`, incl. x86_64 on Apple Silicon via QEMU) | Done |
| Virtio GPU (`--gpu` / `gpu: virtio`) | Done |
| Shared overlay network between VMs (`network: overlay`) | Done |
| Firecracker backend | **Agent production (vFC-1)** on Linux+KVM; **net/mounts still QEMU-only** (vFC-2 later) |

**Firecracker support policy**

| Tier | What you get |
|------|----------------|
| **FC agent production (vFC-1)** | Linux+KVM; pull `fc-kernel` + `grain-ubuntu-fc`; doctor; create `--wait agent`; `grain x` / agent `sh` / cp / sync / MCP guest tools over vsock UDS + `CONNECT` |
| **Not on FC today (use QEMU)** | SSH hostfwd, `grain new -P` / `grain fwd`, overlay, egress proxy hostfwd, 9p/virtiofs mounts, virtio GPU |
| **Later (vFC-2)** | Guest networking / mounts parity path |

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

Use the reported p50/p95 when talking about boot speed — do not invent sub-300ms claims without a local run.
