---
title: "Recipe: coding agent sandbox"
description: How-to recipe from the grain documentation set.
---


Run an agent (or any tool) inside an isolated Linux microVM, with your repo mounted read-write, then copy results back to the host.

## Prerequisites

```bash
grain up
grain image pull          # once
grain doctor              # QEMU + image OK
# Optional but recommended for grain x / grain cp without SSH:
make agent-linux          # from a source checkout
```

## 1. Create a sandbox with the repo mounted

From your project root:

```bash
grain new \
  -n agent \
  -c 4 -m 4096 -d 20 \
  -v "$(pwd):/work" \
  --wait agent
```

| Flag | Why |
|------|-----|
| `-n agent` | stable name for scripts |
| `-c` / `-m` | room for builds and LLMs-in-guest |
| `-v …:/work` | virtio-9p share of the host checkout |
| `--wait agent` | ready when guest agent is healthy (falls back to SSH deploy if needed) |

Persistent disk (keep state across `stop`/`start`):

```bash
grain new -n agent -p -v "$(pwd):/work" --wait agent
```

## 2. Check the guest

```bash
grain agent health agent
grain x agent -- uname -a
grain x agent -- ls -la /work
```

## 3. Run the agent (or your toolchain)

Examples:

```bash
# Install toolchain once (ephemeral VM: re-do after rm)
grain x agent -- sudo apt-get update
grain x agent -- bash -lc 'cd /work && make test'

# Or invoke whatever agent binary/script you use:
grain x agent -- bash -lc 'cd /work && ./scripts/run-agent.sh'
```

Prefer a **profile** for repeated agent work (`~/.grain/config.yaml`):

```yaml
profiles:
  coding-agent:
    cpus: 4
    memory_mb: 4096
    disk_gb: 20
    mounts:
      - {host: ".", guest: "/work"}
```

```bash
grain new --profile coding-agent -n agent --wait agent
```

## 4. Copy results out

Agent-preferred copy (`NAME:path`):

```bash
# guest → host
grain cp agent:/work/out/report.json ./report.json
grain cp agent:/tmp/agent-log.txt ./agent-log.txt

# host → guest
grain cp ./prompts/system.md agent:/work/prompts/system.md
```

Force agent or SSH:

```bash
grain cp --agent agent:/work/out ./out
grain cp --ssh   agent:/work/out ./out
```

List guest paths without SSH:

```bash
grain fs ls agent /work
grain fs stat agent /work/out
```

## 5. Tear down

```bash
grain rm agent          # ephemeral: gone
# or keep disk:
grain stop agent
grain start agent       # later
```

## 6. Optional: allowlisted egress proxy

Keep API tokens on the host and deny other outbound HTTP(S) via the proxy:

```bash
grain proxy up
grain proxy client create agent
grain secret set model-key --value '…'
grain proxy allow --host api.example.com --secret model-key

grain new --proxy -n agent -v "$(pwd):/work" --wait agent
```

Guests use `HTTPS_PROXY=http://TOKEN@10.0.2.2:3128` (SLIRP gateway). See
[proxy.md](../proxy.md) for default-deny rules, listen address, and security notes.

## Tips

- **9p mounts** are great for source; heavy I/O (node_modules, caches) is happier on the guest disk under `/home/ubuntu` or `/var/tmp`.
- First boot with package installs can take a few minutes — `grain logs -f agent`.
- Golden image with agent baked in skips SSH deploy: see [images.md](../images.md) and `scripts/bake-golden.sh`.
- For CI-style one-shot create/exec/rm, see [ci-ephemeral.md](ci-ephemeral.md).
- For host-side default-deny HTTP(S) egress, see [proxy.md](../proxy.md).
