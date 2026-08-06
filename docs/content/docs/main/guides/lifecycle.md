---
title: "Pause, suspend, and restore (lifecycle)"
description: Free guest CPUs or host RAM without losing a persistent lab.
section: guides
keywords:
  - pause
  - resume
  - suspend
  - restore
  - stop
  - start
  - persistent
  - lifecycle
---

{{< only-need href="get-started/quickstart/" >}}
Create, shell, and `grain rm` — no pause/suspend for disposable sandboxes.
{{< /only-need >}}

## Pause vs suspend

| Command | Process | Host RAM | Status | Resume with |
|---------|---------|----------|--------|-------------|
| `grain pause` | QEMU stays up | Still held | `paused` | `grain resume` |
| `grain suspend` | QEMU stopped | Freed | `suspended` | `grain restore` |
| `grain stop` | Stopped | Freed | ephemeral deleted / persistent `stopped` | `grain start` |

**Pause** freezes vCPUs via QMP (`stop` / `cont`). Use it for a short break without tearing down networking.

**Suspend** is for **persistent** VMs only. grain attempts a qcow2 `savevm` snapshot when possible, then stops the process. Restore loads the snapshot if present, otherwise cold-boots the disk.

```bash
grain new -p -n lab
# ... work ...
grain suspend lab
# hours later
grain restore lab
```

## Rules of thumb

- Ephemeral VMs: prefer `rm` or `stop` (they are disposable)  
- Persistent labs: `suspend` / `restore` or `stop` / `start`  
- `start` rejects a `suspended` VM — use `restore`  
- Suspended VMs do not count toward running resource caps  

## Fast create: spawn and warm pool

Cold boots wait on guest UEFI + kernel + agent. Host-side setup is already ~200ms; sub-second (and the &lt;100ms claim path) needs a suspended golden with a qcow2 snapshot and/or a warm pool.

```bash
# Template once
grain new -i grain-ubuntu -n golden -p --wait agent
grain suspend golden

# Spawn = clone + start (clone cost every time)
grain new --from golden -n w1

# Warm pool = pre-cloned members; claim renames + starts (or rename-only if running)
# config.yaml:
#   warm_pool:
#     template: golden
#     size: 2
#     running: false   # true = keep members agent-ready (uses host RAM)
grain pool fill
grain new --from-pool -n w2
grain pool status
grain pool drain   # delete ready members
```

**Default:** pool members stay **stopped/suspended** (disk only, no host RAM). Claim uses `-loadvm` when a suspend snapshot exists.

**Optional `warm_pool.running: true`:** fill starts each member and leaves it agent-ready (RAM cost). Claim is rename/untag only — closest path to “assign a ready sandbox.”

The daemon refills toward `warm_pool.size` after each claim; on `grain up` it also fills in the background when configured.

### Desktop warm-path loop

1. Boot and prepare a golden: create persistent sandbox, wait for agent, then **More → Promote to golden + fill pool** (suspends if running, sets `warm_pool.template` / size, restarts local daemon, fills).
2. Or set **Settings → Warm pool** (template, size, optional running mode) → **Apply warm pool** → **Fill pool**.
3. **New sandbox** prefers **From warm pool** when ready &gt; 0; empty/unconfigured states stay honest (cold boot message, no silent slow path).
4. Multi-select **Start** runs a capacity **preflight** against host caps (`max_vms` / CPU / memory from the active daemon’s `GET /info`).
5. **Activity** drawer can filter by source (`desktop` / `cli` / `mcp` / `api`).

## API

- `POST /vms/{name}/pause`  
- `POST /vms/{name}/resume`  
- `POST /vms/{name}/suspend`  
- `POST /vms/{name}/restore`  
- `POST /vms` body `from` — spawn from template  
- `POST /vms` body `from_pool: true` — claim from warm pool  
- `GET /pool` · `POST /pool/fill` · `POST /pool/claim` · `POST /pool/drain`  
