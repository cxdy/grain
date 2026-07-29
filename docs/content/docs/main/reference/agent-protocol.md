---
title: Agent protocol
description: HTTP endpoints exposed by grain-agent inside each guest.
section: reference
---

The guest agent listens on **TCP `:7475`** (and optionally **vsock port 7475**). The host reaches it via SLIRP hostfwd (`Instance.agent_port`) or AF_VSOCK when configured.

Default version string: see agent package `Version`.

## Health

```http
GET /health
HEAD /health
```

JSON body (GET):

| Field | Meaning |
|-------|---------|
| `hostname` | Guest hostname |
| `agent_version` | Agent version |
| `agent_uptime_sec` | Seconds since agent start |
| `userdata_ran` | True if `/var/lib/grain/userdata-ran` exists |

## Exec

```http
POST /exec?cmd=…&args=…&buffered=true|false&cwd=…&uid=…&gid=…
```

- **`buffered=true`** (default when omitted): single JSON `{stdout,stderr,exit_code,error?}`  
- **`buffered=false`**: NDJSON frames `started` → `stdout`/`stderr` → `exit`  

## Shell

```http
GET /shell?cols=80&rows=24&shell=/bin/bash
```

WebSocket upgrade. Binary frames = PTY I/O. Text JSON resize: `{"type":"resize","cols":N,"rows":M}`. Linux guests only for real PTY.

## Copy

```http
PUT /cp?path=/dest&mode=binary|tar&permissions=0644
GET /cp?path=/src&mode=binary|tar
```

## Filesystem

| Method | Path |
|--------|------|
| GET | `/fs/readdir?path=` |
| GET | `/fs/stat?path=` |
| POST | `/fs/mkdir` body `{"path","recursive","mode"}` |
| DELETE | `/fs/remove?path=&recursive=` |

## Stats

```http
GET /stats
```

Uptime, memory, load averages; disk fields when available.

## Secrets

```http
POST /secrets/materialize
```

Body includes name, base64 data, mode, optional path (default under `/run/grain/secrets/`).

## Host CLI mapping

See [Guest agent guide](/docs/0.2.2/guides/agent/) and [CLI reference](/docs/0.2.2/reference/cli/).
