---
title: "HTTP API reference (REST daemon)"
description: Daemon REST API over unix socket or TCP for automation and the Go/TypeScript SDKs.
section: reference
keywords:
  - API
  - REST
  - OpenAPI
  - unix socket
  - TCP
  - curl
---

## Connect

**Unix socket (default CLI path)**

```bash
curl --unix-socket ~/.grain/grain.sock http://grain/healthz
curl --unix-socket ~/.grain/grain.sock http://grain/vms
```

**TCP** (when `api: 127.0.0.1:7474` is set — default)

```bash
curl -s http://127.0.0.1:7474/healthz
curl -s http://127.0.0.1:7474/openapi.yaml
```

**Auth:** if `api_token` is configured, send `Authorization: Bearer <token>`. `GET /healthz` stays open.

## Machine-readable schema

- **Interactive viewer:** [OpenAPI explorer](../openapi/) (Swagger UI on this site)
- Served live from a running daemon: `GET /openapi.yaml` and `GET /openapi.json`
- Static copy on the site: [`/assets/openapi.yaml`](/assets/openapi.yaml)
- Repo source: [`api/openapi.yaml`](https://github.com/cxdy/grain/blob/main/api/openapi.yaml)

## Common routes

| Method | Path | Notes |
|--------|------|-------|
| GET | `/healthz` | Liveness |
| GET | `/info` | Version |
| GET | `/metrics` | Prometheus text |
| GET | `/openapi.yaml` | Spec |
| GET/POST | `/vms` | List / create |
| GET/DELETE | `/vms/{name}` | Get / delete |
| POST | `/vms/{name}/start` | Start persistent |
| POST | `/vms/{name}/shutdown` | Stop |
| POST | `/vms/{name}/pause` | QMP stop |
| POST | `/vms/{name}/resume` | QMP cont |
| POST | `/vms/{name}/suspend` | Free RAM |
| POST | `/vms/{name}/restore` | From suspended |
| POST | `/vms/{name}/exec` | Agent exec (`buffered=true\|false`) |
| GET | `/vms/{name}/agent/health` | Agent health |
| GET | `/vms/{name}/stats` | Guest stats |
| PUT/GET | `/vms/{name}/cp` | File/tar copy |
| GET/POST/DELETE | `/vms/{name}/fs/*` | readdir, stat, mkdir, remove |
| POST/DELETE | `/vms/{name}/forwards` | Live TCP forwards |
| GET/POST/DELETE | `/secrets` | Host secrets |
| POST | `/vms/{name}/secrets/{secret}` | Inject secret |

### Create query parameters

| Query | Values |
|-------|--------|
| `stream=1` | NDJSON create progress |
| `wait=` | `auto` (empty), `ssh`, `agent`, `userdata` |
| `timeout=` | Go duration, e.g. `3m` |

```bash
curl -N --unix-socket ~/.grain/grain.sock \
  -H 'Accept: application/x-ndjson' \
  -H 'Content-Type: application/json' \
  -d '{"persistent":false}' \
  'http://grain/vms?stream=1&wait=agent&timeout=3m'
```

## SDKs

Prefer typed clients when embedding:

- [Go SDK](../go-sdk/) — [`github.com/cxdy/grain/client`](https://pkg.go.dev/github.com/cxdy/grain/client)
- [TypeScript SDK](../typescript-sdk/) — [`@cxdy/grain`](https://www.npmjs.com/package/@cxdy/grain)
- [Python SDK](../python-sdk/) — [`grainvm`](https://pypi.org/project/grainvm/)

