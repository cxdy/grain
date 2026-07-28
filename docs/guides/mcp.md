---
title: MCP server
description: Connect Claude Code, Codex, OpenCode, Grok Build, and other MCP hosts to grain sandboxes.
---

The **grain MCP server** (`grain-mcp`) exposes sandbox lifecycle tools over the [Model Context Protocol](https://modelcontextprotocol.io/) so coding agents can create, inspect, run commands in, and delete grain microVMs.

It is a thin stdio MCP process that calls a **running grain daemon** via the same HTTP/unix API as the CLI and Go SDK. It does not start QEMU itself.

## Prerequisites

1. Install grain and QEMU ([install]({{ '/get-started/install/' | relative_url }})).
2. Start the daemon:

   ```bash
   grain up
   grain image pull grain-ubuntu   # recommended for agent-ready boots
   ```

3. Build or run the MCP server from a checkout:

   ```bash
   just build-mcp          # → bin/grain-mcp
   # or
   go run ./cmd/grain-mcp
   ```

## Connection env

| Variable | Meaning |
|----------|---------|
| *(default)* | Unix socket `~/.grain/grain.sock` (from config `socket` / data dir) |
| `GRAIN_SOCKET` | Override unix socket path |
| `GRAIN_API` | HTTP base URL for a remote or TCP-bound daemon (e.g. `http://127.0.0.1:7474`) |
| `GRAIN_TOKEN` | Bearer token when the daemon has `api_token` / `auth_token` |
| `GRAIN_CONFIG` | Optional path to grain `config.yaml` |

Same semantics as the grain CLI remote mode.

## Host configuration

### Claude Code / generic MCP (`mcpServers`)

```json
{
  "mcpServers": {
    "grain": {
      "command": "/path/to/grain/bin/grain-mcp",
      "args": [],
      "env": {
        "GRAIN_API": "http://127.0.0.1:7474",
        "GRAIN_TOKEN": ""
      }
    }
  }
}
```

Local socket only (no `GRAIN_API`):

```json
{
  "mcpServers": {
    "grain": {
      "command": "/path/to/grain/bin/grain-mcp",
      "args": []
    }
  }
}
```

### OpenCode / Codex / other stdio hosts

Point the host at the same `command` (absolute path to `grain-mcp` or `go run` wrapper). Transport is **stdio** (JSON-RPC). Ensure `grain up` is running before the host starts the server.

### Grok Build / OpenAPI-compatible tool hosts

If the host supports MCP over stdio, use the same command block. If it only supports OpenAPI HTTP tools, use the grain [HTTP API]({{ '/reference/api/' | relative_url }}) / [OpenAPI]({{ '/reference/openapi/' | relative_url }}) directly instead of `grain-mcp`.

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

Results are JSON text content. API failures surface as MCP tool errors.

## Typical agent flow

1. `grain_health` — confirm daemon  
2. `grain_create_vm` with `image: grain-ubuntu`, `wait: agent`  
3. `grain_exec` — run builds/tests inside the sandbox  
4. `grain_delete_vm` — clean up  

## Limits

- No interactive PTY shell, act recipe, proxy UI, or image bake via MCP (use CLI).
- Create waits for readiness in-process (can take minutes with cold images).
- Hosts must not block stdio; long create/exec holds the tool call until the daemon responds.

## See also

- [HTTP API]({{ '/reference/api/' | relative_url }})
- [Go SDK]({{ '/reference/go-sdk/' | relative_url }})
- [CLI]({{ '/reference/cli/' | relative_url }})
