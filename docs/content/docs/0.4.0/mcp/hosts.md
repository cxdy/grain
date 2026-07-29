---
title: "Configure MCP hosts (Claude, Cursor, Codex…)"
description: Wire Claude Code, Codex, Cursor, Grok Build, OpenCode, and other MCP clients to grain.
section: mcp
keywords:
  - MCP hosts
  - Claude Code
  - Cursor
  - Codex
  - Grok
  - OpenCode
  - stdio
  - Streamable HTTP
---

{{< only-need href="mcp/" >}}
Overview and enablement first if you have not started the MCP server yet.
{{< /only-need >}}

grain MCP speaks **stdio** (`grain mcp`) and **Streamable HTTP** (`grain up --mcp` → `http://127.0.0.1:7476/mcp`). Hosts do **not** share one config format — pick yours, choose a transport, and inject the snippet.

## Enable the daemon

```bash
grain up                 # required for stdio tools
grain up --mcp           # also exposes Streamable HTTP MCP
```

## Agent configs

{{< mcp-hosts >}}

## Tips

- Prefer **stdio** for local IDE agents when the host can spawn a process.
- Long `grain_exec` work can look like a hang — raise the host’s tool timeout (e.g. 600s) and check guest load with `grain_stats`.
- Full tool list: [MCP overview](../).
