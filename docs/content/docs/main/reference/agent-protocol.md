---
title: "Agent protocol (guest HTTP API)"
description: HTTP endpoints exposed by grain-agent inside each guest.
section: reference
keywords:
  - agent protocol
  - grain-agent
  - health
  - exec
  - vsock
---

The guest agent listens on **TCP `:7475`** (and optionally **vsock port 7475**). The host reaches it via SLIRP hostfwd (`Instance.agent_port`) or AF_VSOCK when configured.

**No authentication.** Endpoints accept any caller that can open the port. Host-side isolation is loopback-only hostfwd (and authenticated daemon proxy for remote CLI). On `network: overlay`, peer VMs on the shared L2 can dial each other’s agents — see [Security model](../../explain/security/#guest-agent-trust-model) and [Overlay network](../../guides/networking-overlay/#security-note).

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
| `readiness` | Optional object from the [Readiness protocol](../../explain/readiness/) (`state`, `phase`, `message`, …) |

## Readiness

```http
GET /readiness
```

Returns the same object as `health.readiness` (empty object if no `/var/lib/grain/readiness/` files). See [Readiness protocol](../../explain/readiness/).

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
| POST | `/fs/symlink` body `{"path","target"}` |
| DELETE | `/fs/remove?path=&recursive=` |

`readdir`/`stat` include `target` for symlink entries (link text; not followed).

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

See [Guest agent guide](../../guides/agent/) and [CLI reference](../cli/).
