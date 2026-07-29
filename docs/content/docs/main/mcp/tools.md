---
title: MCP tools
description: Sandbox lifecycle, exec, files, workspace, act, and k3s tools exposed over MCP.
section: mcp
---

The MCP server registers tools such as:

| Tool | Purpose |
|------|---------|
| `grain_health` | Daemon health + info |
| `grain_list_vms` / `grain_get_vm` | Inventory |
| `grain_create_vm` / `grain_start_vm` / `grain_stop_vm` / `grain_delete_vm` | Lifecycle |
| `grain_exec` | Run commands (optional streaming) |
| `grain_write_file` / `grain_read_file` | Guest files |
| `grain_workspace_sandbox` | Repo-oriented sandbox helper |
| `grain_forward_add` / `grain_forward_remove` | Port forwards |
| `grain_image_list` / `grain_image_pull` | Images |
| `grain_fs_*` | Guest filesystem ops |
| `grain_act` / `grain_k3s` | Recipe helpers |
| `grain_agent_health` / `grain_logs` / `grain_stats` | Observe |

Exact names and schemas come from `tools/list` on a live server (`go run ./scripts/mcp-handshake.go ./bin/grain`).

See the [MCP overview](../) for configuration and host setup.
