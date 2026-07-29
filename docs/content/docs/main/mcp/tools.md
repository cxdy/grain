---
title: "MCP tools (lifecycle, exec, sync, act, k3s)"
description: Sandbox lifecycle, exec, files, sync, workspace, act, and k3s tools exposed over MCP.
section: mcp
keywords:
  - MCP tools
  - grain_exec
  - grain_create_vm
  - grain_sync_push
  - grain_sync_pull
  - grain_act
  - grain_k3s
  - tools list
---

{{< only-need href="mcp/hosts/" >}}
Wire your agent first — tool names only matter once the host is connected.
{{< /only-need >}}

The MCP server registers tools such as:

| Tool | Purpose |
|------|---------|
| `grain_health` | Daemon health + info |
| `grain_list_vms` / `grain_get_vm` | Inventory |
| `grain_create_vm` / `grain_start_vm` / `grain_stop_vm` / `grain_delete_vm` | Lifecycle |
| `grain_exec` | Run commands (optional streaming) |
| `grain_write_file` / `grain_read_file` | Guest files |
| `grain_put_tar` / `grain_get_tar` | Tar upload/download (base64) |
| `grain_fs_*` | Guest filesystem ops |
| `grain_sync_push` / `grain_sync_pull` | Unidirectional host↔guest directory sync (same as `grain sync push\|pull`; agent required) |
| `grain_workspace_sandbox` | Repo-oriented sandbox helper |
| `grain_forward_add` / `grain_forward_remove` | Port forwards |
| `grain_image_list` / `grain_image_pull` | Images |
| `grain_act` / `grain_k3s` | Recipe helpers |
| `grain_agent_health` / `grain_logs` / `grain_stats` | Observe |

### Directory sync (`grain_sync_push` / `grain_sync_pull`)

Same core engine as the CLI (`internal/vmsync`). Requires a healthy guest agent; directory roots only.

| Argument | Notes |
|----------|--------|
| `name` | Sandbox / VM name |
| `host_dir` | Absolute host directory root |
| `guest_dir` | Absolute guest directory (e.g. `/work/proj`) |
| `delete` / `dry_run` / `force` | Same semantics as CLI flags |
| `exclude` | Extra gitignore-style patterns |
| `no_defaults` / `no_gitignore` / `no_grainignore` | Ignore control |
| `max_file_size` | Skip oversized source files (bytes) |

Result JSON includes plan counts (`created`, `updated`, `deleted`, `skipped`, `kept_dest`, `conflicts`), `exit_code`, and `applied`. Conflicts set `ok=false` with `exit_code=2` and do not apply.

Exact names and schemas come from `tools/list` on a live server (`go run ./scripts/mcp-handshake.go ./bin/grain`).

See the [MCP overview](../) for configuration and host setup. CLI: [sync push\|pull](../../reference/cli/#grain-sync-push--pull).
