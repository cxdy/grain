---
title: MCP
description: Connect coding agents to grain sandboxes via Model Context Protocol.
section: mcp
---

grain exposes sandbox tools over the [Model Context Protocol](https://modelcontextprotocol.io/) so coding agents can create, inspect, run commands in, and delete microVMs.

MCP is built into the main **`grain` binary**. It talks to a **running grain daemon** via the same HTTP/unix API as the CLI (plus host-local image/log helpers).

## Quick start

The install script can enable MCP by default (`mcp.enabled: true` in `~/.grain/config.yaml`). Otherwise:

```bash
grain up --mcp                 # daemon + Streamable HTTP MCP
# or permanently in ~/.grain/config.yaml:
# mcp:
#   enabled: true
#   listen: 127.0.0.1:7476

grain image pull grain-ubuntu  # or grain_image_pull via MCP
```

Default MCP HTTP endpoint: **`http://127.0.0.1:7476/mcp`**.

For IDE hosts that spawn a stdio server:

```bash
grain mcp                      # stdio (daemon must already be up)
```

## Configuration

```yaml
mcp:
  enabled: false             # true = grain up always starts MCP HTTP
  listen: 127.0.0.1:7476     # Streamable HTTP bind (path /mcp)
```

| Field | Meaning |
|-------|---------|
| `mcp.enabled` | Co-start MCP Streamable HTTP with the daemon |
| `mcp.listen` | `host:port` (default `127.0.0.1:7476`) |

CLI:

| Command | Transport |
|---------|-----------|
| `grain up --mcp` | Daemon + MCP HTTP in one process |
| `grain mcp` | MCP **stdio** (spawn from host config) |
| `grain mcp --http` | MCP HTTP only (daemon must already be up) |
| `grain mcp --http --listen 127.0.0.1:7476` | Override listen address |

## Host configuration

Each agent has its **own** config file and schema. Pick a host, choose **stdio** or **HTTP**, then copy the snippet (and path when it differs by OS).

{{< mcp-hosts >}}

**Before connecting:** `grain up` for stdio tools, or `grain up --mcp` for Streamable HTTP (`http://127.0.0.1:7476/mcp`). Prefer **stdio** when the host can spawn a local process.
## Connection to the daemon

| Variable / config | Meaning |
|-------------------|---------|
| *(default)* | Unix socket `~/.grain/grain.sock` |
| `GRAIN_SOCKET` | Override socket path |
| `GRAIN_API` | HTTP base URL (`http://127.0.0.1:7474`) |
| `GRAIN_TOKEN` | Bearer token when required |

## Tools

### Lifecycle

| Tool | Purpose |
|------|---------|
| `grain_health` | Daemon liveness + version |
| `grain_list_vms` | List sandboxes |
| `grain_get_vm` | Inspect one sandbox |
| `grain_create_vm` | Create VM (**defaults:** `image=grain-ubuntu`, `wait=agent`) |
| `grain_start_vm` / `grain_stop_vm` | Start / stop |
| `grain_delete_vm` | Delete (**idempotent** if missing) |
| `grain_workspace_sandbox` | One-shot: create + mount host dir at `/work` + wait agent + optional first command |

### Guest exec & files

| Tool | Purpose |
|------|---------|
| `grain_exec` | Run command; **streams** stdout/stderr into result by default; `timeout` (default 15m); prefer `go build` over `go test ./...` |
| `grain_write_file` / `grain_read_file` | Write/read guest files (text or base64) |
| `grain_put_tar` / `grain_get_tar` | Upload/download tar (base64) |
| `grain_fs_readdir` / `grain_fs_stat` / `grain_fs_mkdir` / `grain_fs_remove` | Structured guest FS |

### Observability

| Tool | Purpose |
|------|---------|
| `grain_agent_health` | Guest agent `/health` |
| `grain_logs` | Host serial console log (or `qemu=true` for hypervisor log) |
| `grain_stats` | Guest mem/load/disk stats |

### Ports & images

| Tool | Purpose |
|------|---------|
| `grain_forward_add` / `grain_forward_remove` | Live host→guest TCP forwards; add returns `localhost:PORT` |
| `grain_image_list` / `grain_image_pull` | Catalog + local readiness; pull base images on the host |

### Recipes

| Tool | Purpose |
|------|---------|
| `grain_act` | GitHub Actions via **nektos/act** in an ephemeral act-preset sandbox (mounts project at `/work`) |
| `grain_k3s` | **k3s** lab (k3s preset, port 6443, waits for `kubectl get nodes` unless `skip_wait`). Kind is not a separate preset—use k3s. |

### Create parameters (high level)

| Field | Notes |
|-------|--------|
| `name` | Optional; daemon generates if empty |
| `image` | Default **`grain-ubuntu`** |
| `wait` | Default **`agent`** |
| `cpus` / `memory_mb` / `disk_gb` | Resources |
| `persistent` | Keep disk after stop |
| `preset` | `docker` \| `k3s` \| `act` (merges userdata) |
| `publish` | `["8080:80"]` or `["80"]` |
| `mounts` | `["/host/path:/guest/path"]` |
| `timeout` | Go duration for create readiness |

### Exec parameters

| Field | Notes |
|-------|--------|
| `stream` | Default **true** — accumulate streamed stdout/stderr + progress chunks |
| `timeout` | Cap runtime (default **15m**) |
| `cwd` | Optional guest working directory |

## Typical agent flows

**Coding sandbox**

1. `grain_image_list` / `grain_image_pull` if needed  
2. `grain_workspace_sandbox` with `host_dir` = project root  
3. `grain_write_file` / `grain_exec` (focused builds)  
4. `grain_delete_vm` when done  

**GitHub Actions**

```text
grain_act  host_dir=<repo>  act_args=["-j","test"]
```

**k3s lab**

```text
grain_k3s  name=lab  (optional host_dir)
```

## Limits

- No interactive PTY shell, proxy UI, secrets MCP, or golden bake via MCP (use CLI).
- Create/exec can take minutes (cold image, apt, act). Set host tool timeouts accordingly (e.g. 600s).
- Prefer focused builds in-guest; avoid full `go test ./...` unless intentional.

## See also

- [HTTP API](/docs/0.4.0/reference/api/)
- [Go SDK](/docs/0.4.0/reference/go-sdk/)
- [CLI](/docs/0.4.0/reference/cli/)
- [Configuration](/docs/0.4.0/reference/config/)
- [act recipe](/docs/0.4.0/guides/recipes/act/)
- [k3s recipe](/docs/0.4.0/guides/recipes/k3s/)
