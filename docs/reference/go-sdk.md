---
title: Go SDK
description: Public Go client for the grain daemon — DialUnix, create, exec, lifecycle, and more.
---

Package: [`github.com/cxdy/grain/client`](https://pkg.go.dev/github.com/cxdy/grain/client)

This package talks HTTP to a running daemon. It does **not** import `internal/` packages. Types mirror the OpenAPI schema.

## Install

```bash
go get github.com/cxdy/grain/client@latest
```

## Connect

```go
package main

import (
    "context"
    "log"
    "os"
    "path/filepath"

    "github.com/cxdy/grain/client"
)

func main() {
    ctx := context.Background()
    home, _ := os.UserHomeDir()
    sock := filepath.Join(home, ".grain", "grain.sock")

    // Local unix socket (typical CLI setup)
    c, err := client.DialUnix(sock)
    if err != nil {
        log.Fatal(err)
    }

    // With Bearer token when daemon has api_token set:
    // c, err = client.DialUnixToken(sock, os.Getenv("GRAIN_TOKEN"))

    // TCP:
    // c, err = client.DialHTTP("http://127.0.0.1:7474", os.Getenv("GRAIN_TOKEN"))

    if err := c.Health(ctx); err != nil {
        log.Fatal(err)
    }
}
```

## Create with readiness

`Wait` and `Timeout` are **query parameters**, not JSON body fields:

| Wait | Meaning |
|------|---------|
| `""` / `auto` | Daemon chooses (agent for golden images, else SSH) |
| `ssh` | SSH accepts connections |
| `agent` | Guest agent `/health` OK |
| `userdata` | Agent reports userdata finished |

```go
inst, err := c.Create(ctx, client.CreateRequest{
    Name:       "dev",
    Persistent: false,
    CPUs:       2,
    MemoryMB:   2048,
    Wait:       client.WaitAgent,
    Timeout:    "3m",
})

// Progress as NDJSON phases
inst, err = c.CreateStream(ctx, client.CreateRequest{
    Wait: client.WaitSSH,
}, func(ev client.CreateEvent) {
    log.Printf("%s: %s", ev.Phase, ev.Message)
})
```

## Exec and agent

```go
res, err := c.Exec(ctx, inst.Name, "uname", "-a")
// res.Stdout, res.Stderr, res.ExitCode — non-zero exit is not always a Go error

exitCode, err := c.ExecStream(ctx, inst.Name, client.ExecOpts{
    Cmd:  "bash",
    Args: []string{"-lc", "for i in 1 2 3; do echo $i; done"},
}, func(fr client.ExecFrame) error {
    if fr.Type == "stdout" {
        os.Stdout.WriteString(fr.Data)
    }
    return nil
})

h, err := c.AgentHealth(ctx, inst.Name)
st, err := c.Stats(ctx, inst.Name)
```

## Files

```go
f, _ := os.Open("app.tgz")
defer f.Close()
fi, _ := f.Stat()
_ = c.PutFile(ctx, inst.Name, "/tmp/app.tgz", f, fi.Size(), client.CPOpts{})

var buf bytes.Buffer
_ = c.GetFile(ctx, inst.Name, "/etc/os-release", &buf)

entries, _ := c.ReadDir(ctx, inst.Name, "/tmp")
_ = c.Mkdir(ctx, inst.Name, "/opt/app", true, "0755")
```

## Lifecycle

| Method | Behavior |
|--------|----------|
| `Start` | Boot stopped persistent VM |
| `Shutdown` / `Stop` | Stop; ephemeral deleted |
| `Pause` / `Resume` | Freeze / unfreeze vCPUs |
| `Suspend` / `Restore` | Free RAM / restore (persistent) |
| `Delete` | Remove VM |
| `AddForward` / `RemoveForward` | Live TCP forwards |

```go
_ = c.Pause(ctx, name)
_ = c.Resume(ctx, name)
_ = c.Suspend(ctx, name)
inst, err = c.Restore(ctx, name)
lf, err := c.AddForward(ctx, name, 0, 8080) // host port allocated
_ = c.RemoveForward(ctx, name, lf.HostPort)
```

## Secrets

```go
_, _ = c.SetSecret(ctx, client.SecretPut{Name: "k", Data: []byte("value")})
_, _ = c.InjectSecret(ctx, inst.Name, "k", "") // default guest path
_ = c.DeleteSecret(ctx, "k")
```

## Errors

HTTP API errors decode to messages from the JSON `error` field. Always check `err` after dial and mutating calls. For `Exec`, inspect `ExitCode` even when `err == nil`.

## See also

- [HTTP API]({{ '/reference/api/' | relative_url }})  
- [TypeScript SDK]({{ '/reference/typescript-sdk/' | relative_url }})  
- Repo package docs: [`client/README.md`](https://github.com/cxdy/grain/blob/main/client/README.md)  
