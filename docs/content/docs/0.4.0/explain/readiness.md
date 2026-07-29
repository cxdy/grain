---
title: Readiness protocol
description: Contract for custom images and bootstrap authors so grain can report progress and only mark a sandbox ready when you say so.
section: explain
---

Custom images and long first-boot setup need a **shared contract** with grain: how to report *what is happening*, when the sandbox is **done**, and when it **failed**—so `grain new`, create streams, `grain health`, and `grain status` stay accurate.

This page is that contract. **Sandbox recipe files** (declarative create + bootstrap steps) are a later layer that *implements* this protocol; stock goldens already satisfy a compatible subset without changes.

## Goals

1. **Guest-authoritative** — The guest declares progress and done; the host polls and surfaces it.
2. **Compatible by default** — `grain-ubuntu` and current cloud-init keep working with `--wait agent` / `userdata`.
3. **Progress is first-class** — Create progress and status show human-readable bootstrap text.
4. **Fail closed for explicit waits** — `--wait bootstrap` does not return success until the guest says `ready` (or fails hard on `failed` / timeout).

## Guest files

Under **`/var/lib/grain/readiness/`** (create the directory as needed):

| File | Required | Meaning |
|------|----------|---------|
| `state` | yes (for custom bootstrap) | `pending` \| `running` \| `ready` \| `failed` |
| `phase` | optional | Short step id, e.g. `packages`, `configure` |
| `message` | optional | One-line human status |
| `ready_name` | optional | Named condition, e.g. `node20-dev` |
| `updated_at` | optional | RFC3339 timestamp |
| `error` | optional | Detail when `state=failed` |

Existing markers (keep writing them):

| Path | Role |
|------|------|
| `/var/lib/grain/userdata-ran` | Cloud-init / userdata finished (`Health.userdata_ran`) |
| `/var/lib/grain-ready` | Legacy ready stamp |

### State machine

```text
(absent files) ── stock images: host treats as "no protocol"
       │
       ▼
   pending ──► running ──► ready
                  │
                  └──► failed
```

**Custom bootstrap rule:**

1. On start of custom work: write `state=running`, set `phase` / `message`.
2. On each major step: update `phase`, `message`, optionally `updated_at`.
3. On success: `state=ready` (optional `ready_name=…`).
4. On failure: `state=failed` and `error=…`.

### Shell helper (no extra binary required)

```bash
# grain-ready-report <state> [phase] [message]
grain-ready-report() {
  local state="$1" phase="${2:-}" message="${3:-}"
  local dir=/var/lib/grain/readiness
  mkdir -p "$dir"
  printf '%s\n' "$state" >"$dir/state"
  [ -n "$phase" ] && printf '%s\n' "$phase" >"$dir/phase" || rm -f "$dir/phase"
  [ -n "$message" ] && printf '%s\n' "$message" >"$dir/message" || rm -f "$dir/message"
  date -u +"%Y-%m-%dT%H:%M:%SZ" >"$dir/updated_at"
  if [ "$state" = "failed" ] && [ -n "$message" ]; then
    printf '%s\n' "$message" >"$dir/error"
  fi
}

grain-ready-report running packages "apt-get install …"
# … work …
grain-ready-report ready
# or: grain-ready-report failed "" "setup.sh exit 1"
```

### cloud-init example

Append runcmd that drives readiness, and only stamp `userdata-ran` after success (or stamp userdata early and keep bootstrap in `readiness/` until ready):

```yaml
#cloud-config
runcmd:
  - |
    set -e
    dir=/var/lib/grain/readiness
    mkdir -p "$dir" /var/lib/grain
    echo running >"$dir/state"
    echo packages >"$dir/phase"
    echo "installing packages" >"$dir/message"
    export DEBIAN_FRONTEND=noninteractive
    apt-get update -qq
    apt-get install -y -qq git
    echo ready >"$dir/state"
    echo node20-dev >"$dir/ready_name"
    touch /var/lib/grain/userdata-ran
```

## Agent API

`GET /health` (and `grain health` / daemon proxy) includes an optional `readiness` object when files are present:

```json
{
  "hostname": "sbox-1",
  "agent_version": "0.3.0",
  "agent_uptime_sec": 42,
  "userdata_ran": true,
  "readiness": {
    "state": "running",
    "phase": "packages",
    "message": "installing git",
    "ready_name": "node20-dev",
    "updated_at": "2026-07-29T12:00:01Z",
    "error": ""
  }
}
```

`GET /readiness` returns the same object alone (empty object if no protocol files).

## Host behavior

### Create phases

```text
image → disk → seed → qemu → wait_ssh | wait_agent → [userdata] → [bootstrap] → ready | error
```

| Phase | Meaning |
|-------|---------|
| `userdata` | Waiting for `userdata_ran` |
| `bootstrap` | Waiting for readiness `state=ready` (messages come from the guest) |
| `ready` | Create succeeded |

### Wait modes (`grain new --wait`, API `wait=`)

| Mode | Behavior |
|------|----------|
| `auto` / `ssh` / `agent` | Unchanged (no forced bootstrap wait) |
| `userdata` | Agent + `userdata_ran` |
| **`bootstrap`** | Agent, then poll readiness until `ready`, or fail on `failed` / timeout |

**Missing `readiness/` files when `wait=bootstrap`:** treated as **pending** until timeout (authors must stamp `ready`).

**On `state=failed` or timeout:** create returns an **error**; the **VM is left running** so you can `grain logs`, `grain status`, `grain sh`, or `grain rm`.

### CLI

```bash
grain new --wait bootstrap -n lab
grain health lab          # full JSON including readiness
grain status lab          # one-liner: status + agent + readiness
```

Example status line:

```text
lab  status=running  image=grain-ubuntu  agent=up  userdata=ran  readiness=running  phase=packages  "installing git"
```

## Stock images

Golden **`grain-ubuntu`** continues to stamp `userdata-ran` quickly and does **not** require `readiness/` files. Use `--wait agent` (default for goldens) as today.

Use **`--wait bootstrap`** only when your image or userdata implements this protocol.

## Relation to profiles, presets, and docs recipes

| Concept | Role |
|---------|------|
| **Profile** | Named create defaults in config |
| **Preset** | Embedded cloud-init fragments (`docker`, `k3s`, `act`) |
| **Docs “recipes”** | Human guides / one-shot CLIs (`grain act`) |
| **Readiness protocol** | Guest contract for progress + done (this page) |
| **Sandbox recipe file** (future) | Portable YAML that drives create + bootstrap *using* this protocol |

## See also

- [Guest agent](/docs/0.4.0/guides/agent/)
- [Profiles and presets](/docs/0.4.0/guides/profiles/)
- [Images and golden boots](/docs/0.4.0/guides/images/)
- [CLI](/docs/0.4.0/reference/cli/)
