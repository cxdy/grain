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

## Client identification

Mutating clients should send `User-Agent` and/or `X-Grain-Client` with one of: `cli`, `desktop`, `mcp`, `sdk`. The daemon records this on [activity](#activity) events so Desktop can filter by source.

## Common routes

| Method | Path | Notes |
|--------|------|-------|
| GET | `/healthz` | Liveness |
| GET | `/info` | Version + **resource caps** (see below) |
| GET | `/metrics` | Prometheus text |
| GET | `/activity` | Recent control-plane activity ring |
| GET | `/openapi.yaml` | Spec |
| GET/POST | `/vms` | List / create |
| GET/DELETE | `/vms/{name}` | Get / delete |
| POST | `/vms/{name}/start` | Start persistent |
| POST | `/vms/{name}/shutdown` | Stop |
| POST | `/vms/{name}/clone` | Offline clone (stopped/suspended source) |
| POST | `/vms/{name}/pause` | QMP stop |
| POST | `/vms/{name}/resume` | QMP cont |
| POST | `/vms/{name}/suspend` | Free RAM |
| POST | `/vms/{name}/restore` | From suspended |
| POST | `/vms/{name}/exec` | Agent exec (`buffered=true\|false`) |
| GET | `/vms/{name}/agent/health` | Agent health |
| POST | `/vms/{name}/agent/deploy` | Deploy/refresh agent over SSH (binary on daemon host) |
| GET | `/vms/{name}/stats` | Guest stats |
| GET | `/vms/{name}/metrics` | Host-side guest stats history (`metrics.ring`) when enabled |
| PUT/GET | `/vms/{name}/cp` | File/tar copy |
| GET/POST/DELETE | `/vms/{name}/fs/*` | readdir, stat, mkdir, remove |
| POST/DELETE | `/vms/{name}/forwards` | Live TCP forwards |
| GET | `/pool` | Warm pool inventory |
| POST | `/pool/fill` | Fill pool to configured size |
| POST | `/pool/claim` | Claim one member (`{"name":"…"}` optional) |
| POST | `/pool/drain` | Delete ready pool members |
| GET/POST/DELETE | `/secrets` | Host secrets |
| POST | `/vms/{name}/secrets/{secret}` | Inject secret |

### `GET /info`

JSON object of **strings** (stable for simple clients):

| Key | Meaning |
|-----|---------|
| `name` | `"grain"` |
| `version` | Daemon version |
| `max_vms` | Cap on concurrent running/creating VMs (`0` = unlimited) |
| `max_cpus_total` | Sum of vCPUs across running/creating |
| `max_memory_mb_total` | Sum of MemoryMB across running/creating |
| `max_cpus_per_vm` | Per-VM CPU cap |
| `max_memory_mb_per_vm` | Per-VM memory cap |

Desktop bulk-start **preflight** reads these from the **active** host (local or remote).

### Activity

`GET /activity?since=<id>&limit=N` returns recent control-plane mutations (create/start/stop/rm/exec/pool/…). Events include `source` (`cli` / `desktop` / `mcp` / `sdk` / `api`), `action`, `target`, `status`, timings. Ring is persisted under `data_dir/activity.json` across daemon restarts.

### Warm pool

| Method | Path | Body / notes |
|--------|------|----------------|
| GET | `/pool` | `{enabled, template, desired, ready, members, running}` |
| POST | `/pool/fill` | Clone until ready == desired |
| POST | `/pool/claim` | Optional `{"name":"work-1"}` |
| POST | `/pool/drain` | Deletes ready members |

Config: `warm_pool.template` / `size` / `running` — [config reference](../config/).

### Create query parameters

| Query | Values |
|-------|--------|
| `stream=1` | NDJSON create progress |
| `wait=` | `auto` (empty), `ssh`, `agent`, `userdata`, `bootstrap` |
| `timeout=` | Go duration, e.g. `3m` |

### Create body (highlights)

| Field | Notes |
|-------|--------|
| `name`, `image`, `cpus`, `memory_mb`, `disk_gb`, `persistent` | Standard create |
| `from` | Spawn from stopped/suspended template (fast `-loadvm` when snapshotted) |
| `from_pool` | Claim warm-pool member (`true`; mutually exclusive with `from`) |
| `metrics_enabled` | Host-side guest stats ring (default follows daemon config; usually on) |
| `wait` / timeout | Also via query string |

```bash
# Cold create with progress
curl -N --unix-socket ~/.grain/grain.sock \
  -H 'Accept: application/x-ndjson' \
  -H 'Content-Type: application/json' \
  -H 'X-Grain-Client: cli' \
  -d '{"persistent":false}' \
  'http://grain/vms?stream=1&wait=agent&timeout=3m'

# Fast spawn from suspended template
curl --unix-socket ~/.grain/grain.sock -H 'Content-Type: application/json' \
  -d '{"from":"golden","name":"work1"}' http://grain/vms

# Claim from warm pool
curl --unix-socket ~/.grain/grain.sock -H 'Content-Type: application/json' \
  -d '{"from_pool":true,"name":"work2"}' http://grain/vms
```

## SDKs

Prefer typed clients when embedding:

- [Go SDK](../go-sdk/) — [`github.com/cxdy/grain/client`](https://pkg.go.dev/github.com/cxdy/grain/client)
- [TypeScript SDK](../typescript-sdk/) — [`@cxdy/grain`](https://www.npmjs.com/package/@cxdy/grain)
- [Python SDK](../python-sdk/) — [`grainvm`](https://pypi.org/project/grainvm/)

