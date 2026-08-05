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

## API

- `POST /vms/{name}/pause`  
- `POST /vms/{name}/resume`  
- `POST /vms/{name}/suspend`  
- `POST /vms/{name}/restore`  
