---
title: Python SDK
description: Stdlib Python client for the grain daemon — TCP or Unix socket, create, exec, lifecycle, and more.
---

PyPI: **[`grainvm`](https://pypi.org/project/grainvm/)** (import as `grain`).  
Source: [`sdk/python`](https://github.com/cxdy/grain/tree/main/sdk/python).

## Install

```bash
pip install grainvm
```

Requires **Python 3.9+**. **Zero runtime dependencies** (stdlib `http.client` only).

## Quick start (TCP)

Ensure the daemon is up with TCP API enabled (`api: 127.0.0.1:7474` is the default config).

```python
from grain import GrainClient, CreateRequest, WAIT_AGENT

grain = GrainClient(
    base_url="http://127.0.0.1:7474",
    token=__import__("os").environ.get("GRAIN_TOKEN", ""),
)

grain.health()

inst = grain.create(
    CreateRequest(persistent=False),
    wait=WAIT_AGENT,
    timeout="3m",
)

print(inst.name, inst.status, inst.ssh_port)

result = grain.exec(inst.name, "uname", ["-a"])
print(result.stdout.strip(), "exit", result.exit_code)

agent = grain.agent_health(inst.name)
print(agent.agent_version, agent.hostname)

stats = grain.stats(inst.name)
print("load1", stats.load1)

grain.delete(inst.name)
```

## Connect options

```python
GrainClient(
    base_url="http://127.0.0.1:7474",
    token="",                 # Authorization: Bearer
    socket_path=None,         # e.g. ~/.grain/grain.sock
    timeout=300.0,            # seconds
)

# Helpers
GrainClient.unix(str(Path.home() / ".grain" / "grain.sock"), token="...")
GrainClient.http("http://127.0.0.1:7474", token="...")
```

### Unix socket

```python
from pathlib import Path
from grain import GrainClient

grain = GrainClient.unix(str(Path.home() / ".grain" / "grain.sock"))
grain.health()
```

## Methods (overview)

| Area | Methods |
|------|---------|
| System | `health()`, `info()` |
| VMs | `list`, `create`, `create_stream`, `get`, `delete` |
| Lifecycle | `start`, `stop` / `shutdown`, `pause`, `resume`, `suspend`, `restore` |
| Forwards | `add_forward`, `remove_forward` |
| Agent | `exec`, `exec_stream`, `agent_health`, `stats` |
| Files | `put_file`, `get_file`, `readdir`, `stat`, `mkdir`, `remove` |

### Create wait modes

`wait` and `timeout` are **query parameters**, not JSON body fields:

| Wait | Meaning |
|------|---------|
| `""` / `auto` | Daemon chooses (agent for golden images, else SSH) |
| `ssh` | SSH accepts connections |
| `agent` | Guest agent `/health` OK |
| `userdata` | Agent reports userdata finished |

Constants: `WAIT_AUTO`, `WAIT_SSH`, `WAIT_AGENT`, `WAIT_USERDATA`.

### Streaming create

```python
inst = grain.create_stream(
    CreateRequest(persistent=False, wait=None),
    on_event=lambda ev: print(ev.phase, ev.message or ""),
    wait="ssh",
)
```

### Streaming exec

```python
from grain import ExecOpts

code = grain.exec_stream(
    inst.name,
    ExecOpts(cmd="sh", args=["-c", "for i in 1 2 3; do echo $i; done"]),
    on_frame=lambda f: print(f.data or "", end="") if f.type == "stdout" else None,
)
```

## Errors

Non-2xx responses raise `GrainAPIError` with `.status` and optional `.body`. Buffered `exec` returns non-zero guest exit codes in `ExecResult.exit_code` without raising.

## See also

- [Go SDK]({{ '/reference/go-sdk/' | relative_url }})
- [TypeScript SDK]({{ '/reference/typescript-sdk/' | relative_url }})
- [HTTP API]({{ '/reference/api/' | relative_url }})
- OpenAPI: [`api/openapi.yaml`](https://github.com/cxdy/grain/blob/main/api/openapi.yaml)
