---
title: "Configure MCP hosts"
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
Read the overview first if you have not started the MCP server yet.
{{< /only-need >}}

grain MCP supports **stdio** (`grain mcp`) and **Streamable HTTP** (`grain up --mcp` → `http://127.0.0.1:7476/mcp`). Hosts do not share one config format — pick yours, choose a transport, and paste the snippet.

## Enable the daemon

```bash
grain up                 # required for stdio tools
grain up --mcp           # also exposes Streamable HTTP MCP
```

## Agent configs

{{< mcp-hosts >}}

## Tips

- Prefer **stdio** for local IDE agents that can spawn a process.
- Long `grain_exec` calls can look hung — raise the host tool timeout (for example 600s) and check guest load with `grain_stats`.
- Full tool list: [MCP overview](../).
