---
title: "Bootstrap until ready"
description: "Run packages or a setup script before grain says the sandbox is ready — with status you can watch."
section: get-started
keywords:
  - bootstrap
  - readiness
  - userdata
  - apt
  - setup script
  - wait bootstrap
  - custom image
---

Create a VM, run your install/setup steps, and only get a shell (or next command) when those steps finished **with zero failures**.

Stock `grain-ubuntu` is already ready when the agent is up. This page is for **custom** first-boot work (apt, scripts, image authors).

Full readiness protocol: [Readiness protocol](../explain/readiness/).

---

## What “ready” means here

A sandbox is ready when:

1. The **VM is up** and the **guest agent** is healthy, and
2. Your **bootstrap** finished successfully (`state=ready`), with **no failed steps**.

If bootstrap fails, create errors and the VM is **left running** so you can inspect logs and fix.

---

## Minimal path (cloud-init)

Save this as `bootstrap.yaml` on the **host**:

```yaml
#cloud-config
runcmd:
  - |
    set -euo pipefail
    dir=/var/lib/grain/readiness
    mkdir -p "$dir" /var/lib/grain

    echo running >"$dir/state"
    echo packages >"$dir/phase"
    echo "installing git" >"$dir/message"

    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq git

    echo ready >"$dir/state"
    echo my-stack >"$dir/ready_name"
    date -u +"%Y-%m-%dT%H:%M:%SZ" >"$dir/updated_at"
    touch /var/lib/grain/userdata-ran
```

Create and **wait for bootstrap**:

```bash
grain up
grain image pull grain-ubuntu   # or your custom image
grain new --userdata-file ./bootstrap.yaml --wait bootstrap -n lab
```

While it runs, create progress should mention bootstrap / your `message`. When the command returns successfully, bootstrap completed with no failures.

```bash
grain status lab
grain health lab
grain sh lab
```

---

## Report progress from a script

Inside the guest (or in runcmd), use the same files — or copy [scripts/grain-ready-report.sh](https://github.com/cxdy/grain/blob/main/scripts/grain-ready-report.sh) into the image:

```bash
grain-ready-report running packages "apt-get install …"
# … work …
grain-ready-report ready
# on error:
# grain-ready-report failed "" "setup.sh exit 1"
```

Files live under `/var/lib/grain/readiness/` (`state`, `phase`, `message`, `error`, …).

---

## Watch status

```bash
grain status lab
# lab  status=running  …  readiness=running  phase=packages  "installing git"

grain health lab   # full JSON including readiness
```

---

## Common failures

| Symptom | Check |
|---------|--------|
| Create hangs on bootstrap | Guest never wrote `state=ready` — look at `grain logs -f lab` |
| Create errors, VM still there | Bootstrap wrote `state=failed` or timed out — `grain status` / logs, then `grain rm lab` |
| Stock image, no custom setup | Use default wait (`agent` for goldens); you don’t need this page |

---

## Next

- Prefer a checked-in YAML file? [Sandbox recipes](../recipe/) (`grain new --recipe`)
- States, wait modes, agent API: [Readiness protocol](../explain/readiness/)
- Named create defaults: [Profiles & presets](../guides/profiles/)
- Baking faster images later: [Images](../guides/images/)
