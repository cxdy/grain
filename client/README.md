# grain Go client

Public SDK for the grain daemon HTTP API. Types mirror [`api/openapi.yaml`](../api/openapi.yaml).
This package does **not** import `internal/`.

## Connect

```go
import (
    "path/filepath"
    "os"
    "github.com/cxdy/grain/client"
)

// Local CLI path (Unix socket)
c, err := client.DialUnix(filepath.Join(os.Getenv("HOME"), ".grain", "grain.sock"))

// Optional Bearer token (when daemon has api_token configured)
c, err = client.DialUnixToken(sock, os.Getenv("GRAIN_TOKEN"))

// TCP bind
c, err = client.DialHTTP("http://127.0.0.1:7474", os.Getenv("GRAIN_TOKEN"))
```

## Create with readiness wait

`Wait` and `Timeout` are query parameters (not JSON body fields):

| Wait value | Meaning |
|------------|---------|
| `""` / `auto` | Daemon default (agent for golden images, else SSH) |
| `ssh` | Ready when SSH accepts connections |
| `agent` | Ready when guest grain-agent `/health` succeeds |
| `userdata` | Ready when agent reports userdata finished |
| `bootstrap` | Ready when guest readiness protocol reports `state=ready` |

Sandbox recipes (`kind: Sandbox`) are compiled by the CLI (`grain new --recipe`)
into create options + userdata; they are not a separate HTTP API resource.

```go
inst, err := c.Create(ctx, client.CreateRequest{
    Name:       "dev",
    Persistent: false,
    Wait:       client.WaitAgent, // or "agent"
    Timeout:    "3m",
})

// NDJSON progress
inst, err = c.CreateStream(ctx, client.CreateRequest{
    Wait: client.WaitSSH,
}, func(ev client.CreateEvent) {
    log.Printf("%s: %s", ev.Phase, ev.Message)
})
```

## Lifecycle

| Method | API | Notes |
|--------|-----|--------|
| `Start` | `POST /vms/{name}/start` | Boot a stopped persistent VM |
| `Shutdown` / `Stop` | `POST /vms/{name}/shutdown` | Ephemeral deleted; persistent stopped |
| `Pause` | `POST /vms/{name}/pause` | QMP stop — freezes vCPUs, QEMU stays up |
| `Resume` | `POST /vms/{name}/resume` | QMP cont |
| `Suspend` | `POST /vms/{name}/suspend` | Persistent only — stops process, frees host RAM; optional qcow2 savevm |
| `Restore` | `POST /vms/{name}/restore` | From `suspended` — loadvm when snapshot exists, else cold boot |
| `Delete` | `DELETE /vms/{name}` | Remove VM |
| `PoolStatus` / `PoolFill` / `PoolClaim` / `PoolDrain` | `GET/POST /pool…` | Warm pool of pre-cloned suspended templates; claim = rename + start |
| Create `From` / `FromPool` | `POST /vms` | Spawn from template or claim from pool |

```go
_ = c.Pause(ctx, name)
_ = c.Resume(ctx, name)

// Free host RAM on a persistent VM (differs from Pause)
_ = c.Suspend(ctx, name)
inst, err = c.Restore(ctx, name)
```

## Live port forwards

Runtime SSH tunnels (`ssh -N -L`), not SLIRP create-time hostfwds.
Cleared on stop/delete.

```go
lf, err := c.AddForward(ctx, name, 0, 8080) // host_port 0 = allocate
// lf.HostPort is the resolved host port
_ = c.RemoveForward(ctx, name, lf.HostPort)
```

## Guest agent

```go
res, err := c.Exec(ctx, name, "uname", "-a")
code, err := c.ExecStream(ctx, name, client.ExecOpts{Cmd: "sh", Args: []string{"-c", "echo hi"}}, onFrame)
h, err := c.AgentHealth(ctx, name)
st, err := c.Stats(ctx, name)

_ = c.PutFile(ctx, name, "/tmp/x", r, size, client.CPOpts{Mode: "0644"})
_ = c.GetFile(ctx, name, "/tmp/x", w)
entries, err := c.ReadDir(ctx, name, "/tmp")
```

## Auth

When the daemon has `api_token` / `auth_token`, all routes except `GET /healthz`
require `Authorization: Bearer <token>`. Use `DialHTTP`/`DialUnixToken` or `SetToken`.
