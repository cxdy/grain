---
title: MCP server
description: Connect Claude Code, Codex, OpenCode, Grok Build, and other MCP hosts to grain sandboxes.
---

grain exposes sandbox tools over the [Model Context Protocol](https://modelcontextprotocol.io/) so coding agents can create, inspect, run commands in, and delete microVMs.

MCP is built into the main **`grain` binary**. It talks to a **running grain daemon** via the same HTTP/unix API as the CLI.

## Quick start

The install script can enable MCP by default (`mcp.enabled: true` in `~/.grain/config.yaml`). Otherwise:

```bash
grain up --mcp                 # daemon + Streamable HTTP MCP
# or permanently in ~/.grain/config.yaml:
# mcp:
#   enabled: true
#   listen: 127.0.0.1:7476

grain image pull grain-ubuntu
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

### Stdio (Claude Code, Codex, OpenCode, …)

```json
{
  "mcpServers": {
    "grain": {
      "command": "grain",
      "args": ["mcp"]
    }
  }
}
```

Use an absolute path to `grain` if it is not on the host’s `PATH`. Ensure `grain up` (optionally with `--mcp`) is running first.

### Streamable HTTP

Point MCP HTTP clients at:

```text
http://127.0.0.1:7476/mcp
```

after `grain up --mcp` or `mcp.enabled: true`.

## Connection to the daemon

When tools run, the MCP layer dials the daemon like the CLI:

| Variable / config | Meaning |
|-------------------|---------|
| *(default)* | Unix socket `~/.grain/grain.sock` |
| `GRAIN_SOCKET` | Override socket path |
| `GRAIN_API` | HTTP base URL (`http://127.0.0.1:7474`) |
| `GRAIN_TOKEN` | Bearer token when required |
| `api` / `api_token` | Daemon listen + auth (config) |

## Tools

| Tool | Daemon API | Purpose |
|------|------------|---------|
| `grain_health` | `GET /healthz`, `GET /info` | Daemon liveness and version |
| `grain_list_vms` | `GET /vms` | List sandboxes |
| `grain_get_vm` | `GET /vms/{name}` | Inspect one sandbox |
| `grain_create_vm` | `POST /vms` | Create (name, image, cpus, memory_mb, disk_gb, persistent, arch, gpu, network, wait, timeout, userdata, publish[], mounts[]) |
| `grain_start_vm` | `POST /vms/{name}/start` | Start stopped persistent VM |
| `grain_stop_vm` | `POST /vms/{name}/shutdown` | Stop (ephemeral deleted) |
| `grain_delete_vm` | `DELETE /vms/{name}` | Delete sandbox |
| `grain_exec` | `POST /vms/{name}/exec` | Buffered guest command (`cmd` + `args`) |

### Create parameters (high level)

| Field | Notes |
|-------|--------|
| `name` | Optional; daemon generates if empty |
| `image` | e.g. `grain-ubuntu`, `ubuntu-cloud`, `auto` |
| `cpus` / `memory_mb` / `disk_gb` | Resources |
| `persistent` | Keep disk after stop |
| `wait` | `auto` \| `ssh` \| `agent` \| `userdata` |
| `timeout` | Go duration string, e.g. `3m` |
| `publish` | `["8080:80"]` or `["80"]` |
| `mounts` | `["/host/path:/guest/path"]` |
| `userdata` | Cloud-init string |

## Typical agent flow

1. `grain_health` — confirm daemon  
2. `grain_create_vm` with `image: grain-ubuntu`, `wait: agent`  
3. `grain_exec` — run builds/tests inside the sandbox  
4. `grain_delete_vm` — clean up  

## Limits

- No interactive PTY shell, act recipe, proxy UI, or image bake via MCP (use CLI).
- Create waits for readiness in-process (can take minutes with cold images).
- If the daemon is already up without MCP, use `grain mcp --http` or restart with `grain down && grain up --mcp`.

## See also

- [HTTP API]({{ '/reference/api/' | relative_url }})
- [Go SDK]({{ '/reference/go-sdk/' | relative_url }})
- [CLI]({{ '/reference/cli/' | relative_url }})
- [Configuration]({{ '/reference/config/' | relative_url }})
