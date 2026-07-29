---
title: "Ephemeral CI (create → test → rm)"
description: "Disposable Linux runner: one VM per job, no leftover state."
section: guides
keywords:
  - CI
  - ephemeral
  - recipe
  - automation
  - create destroy
---

{{< only-need href="get-started/quickstart/" >}}
Manual create/shell/rm first — this page is a scripted CI pattern.
{{< /only-need >}}

Use grain as a disposable Linux runner: one VM per job, no leftover state.

## Prerequisites

```bash
grain up
grain image pull
# Optional for agent exec without SSH latency:
# just agent-linux   # from a checkout, once per arch
```

## Minimal script

```bash
#!/usr/bin/env bash
set -euo pipefail

NAME="ci-$$"
ROOT="$(cd "$(dirname "$0")/.." && pwd)"   # or: REPO checkout path

cleanup() {
  grain rm "$NAME" 2>/dev/null || true
}
trap cleanup EXIT

grain new \
  -n "$NAME" \
  -c 2 -m 2048 \
  -v "${ROOT}:/work" \
  --wait agent

grain x "$NAME" -- bash -lc 'cd /work && ./scripts/ci.sh'
# exit code of grain x matches the remote command
```

Save as `scripts/grain-ci.sh`, `chmod +x`, run from CI or a laptop.

## Wait modes

| `--wait` | Ready when | Use when |
|----------|------------|----------|
| `ssh` (default) | SSH accepts connections | fastest; agent deploy is best-effort |
| `agent` | guest agent `/health` OK | `grain x` / `cp` / `fs` without SSH |
| `userdata` | agent reports userdata finished | package installs in cloud-init must complete first |

Ephemeral CI usually wants `--wait agent` so `grain x` is reliable immediately after create.

## Install deps then test

```bash
grain new -n "$NAME" -v "${ROOT}:/work" --wait agent

grain x "$NAME" -- sudo apt-get update -qq
grain x "$NAME" -- sudo apt-get install -y -qq build-essential git
grain x "$NAME" -- bash -lc 'cd /work && make test'
```

Or bake deps into userdata / a golden image so every job skips apt:

```bash
grain new -n "$NAME" -i grain-ubuntu -v "${ROOT}:/work" --wait agent
grain x "$NAME" -- bash -lc 'cd /work && make test'
```

See [images.md](../images.md) for `grain image import` and `scripts/bake-golden.sh`.

## Capture artifacts

```bash
grain x "$NAME" -- bash -lc 'cd /work && make test | tee /tmp/test.log'
grain cp "$NAME":/tmp/test.log "./test-${NAME}.log"
# junit / coverage:
grain cp "$NAME":/work/coverage.out "./coverage-${NAME}.out"
```

## GitHub Actions sketch

```yaml
jobs:
  grain-test:
    runs-on: macos-latest   # or Linux with KVM/QEMU
    steps:
      - uses: actions/checkout@v4
      - name: Install grain
        run: curl -fsSL https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh | bash
      - name: QEMU
        run: brew install qemu   # adjust for Linux runners
      - name: Agent binary (optional)
        run: |
          # if building from this monorepo; otherwise skip and use SSH-only x
          just agent-linux || true
      - name: Up + image
        run: |
          grain doctor || true
          grain up
          grain image pull
      - name: Test in microVM
        run: |
          set -euo pipefail
          NAME="gha-$GITHUB_RUN_ID"
          trap 'grain rm "$NAME" 2>/dev/null || true' EXIT
          grain new -n "$NAME" -v "$PWD:/work" --wait agent
          grain x "$NAME" -- bash -lc 'cd /work && make test'
```

## Parallel jobs

Each VM needs unique `-n` and its own host ports (SSH/agent auto-allocate). Example:

```bash
for i in 1 2 3; do
  (
    N="ci-$i-$$"
    grain new -n "$N" -v "$PWD:/work" --wait agent
    grain x "$N" -- bash -lc "cd /work && make test-shard SHARD=$i"
    grain rm "$N"
  ) &
done
wait
```

Watch resource caps in `~/.grain/config.yaml` (`max_vms`, `max_cpus_total`, …).

## Cleanup guarantees

- Ephemeral VMs are deleted on `grain rm`, `grain stop`, and daemon `grain down`.
- Always `trap cleanup EXIT` so failures still `rm`.
- `grain ls` should be empty after the job if every path hits the trap.

## Related

- [coding-agent.md](coding-agent.md) — long-lived mounted workspace
- [docker-socket.md](docker-socket.md) — Docker inside the job VM
- [troubleshooting.md](../troubleshooting.md) — boot timeouts, doctor
