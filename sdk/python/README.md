# `cxdy-grain` — Python client

Lightweight Python SDK for the [grain](https://github.com/cxdy/grain) daemon HTTP API. Stdlib only (`http.client`) — no runtime dependencies. Types and methods mirror the [Go client](https://pkg.go.dev/github.com/cxdy/grain/client) and [TypeScript SDK](https://www.npmjs.com/package/@cxdy/grain).

## Install

```bash
pip install cxdy-grain
```

Package: [pypi.org/project/cxdy-grain](https://pypi.org/project/cxdy-grain/). Requires **Python 3.9+**.

From a grain checkout (development):

```bash
pip install -e /path/to/grain/sdk/python
```

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
    CreateRequest(persistent=False, cpus=2, memory_mb=2048),
    wait=WAIT_AGENT,
    timeout="3m",
)
print(inst.name, inst.status, inst.ssh_port)

result = grain.exec(inst.name, "uname", ["-a"])
print(result.stdout.strip(), "exit", result.exit_code)

health = grain.agent_health(inst.name)
print(health.agent_version, health.hostname)

stats = grain.stats(inst.name)
print("load1", stats.load1)

grain.delete(inst.name)
```

## Unix socket

```python
from pathlib import Path
from grain import GrainClient

grain = GrainClient.unix(
    str(Path.home() / ".grain" / "grain.sock"),
    token=__import__("os").environ.get("GRAIN_TOKEN", ""),
)
grain.health()
print(grain.list())
```

## Constructor

```python
GrainClient(
    base_url="http://127.0.0.1:7474",  # TCP
    token="",                          # Authorization: Bearer
    socket_path=None,                  # Unix socket path
    timeout=300.0,                     # socket timeout (seconds)
)

GrainClient.unix("~/.grain/grain.sock", token="...")
GrainClient.http("http://127.0.0.1:7474", token="...")
```

## Methods

| Method | Daemon route |
|--------|----------------|
| `health()` | `GET /healthz` |
| `info()` | `GET /info` |
| `list()` | `GET /vms` |
| `create(req, wait=…, timeout=…)` | `POST /vms` |
| `create_stream(req, on_event, …)` | `POST /vms?stream=1` (NDJSON) |
| `get(name)` / `delete(name)` | `GET` / `DELETE /vms/{name}` |
| `start` / `stop` / `shutdown` | lifecycle |
| `pause` / `resume` / `suspend` / `restore` | QMP / disk |
| `add_forward` / `remove_forward` | live port forwards |
| `exec` / `exec_stream` | guest agent exec |
| `agent_health` / `stats` | agent probes |
| `readdir` / `stat` / `mkdir` / `remove` | guest fs |
| `put_file` / `get_file` | guest file copy |

Errors raise `GrainAPIError` with `status` and optional `body`. Buffered `exec` returns non-zero guest exit codes in `ExecResult.exit_code` without raising.

### Streamed create

```python
inst = grain.create_stream(
    CreateRequest(persistent=False, cpus=2),
    on_event=lambda ev: print(ev.phase, ev.message or ""),
    wait="ssh",
)
```

### Streamed exec

```python
from grain import ExecOpts

code = grain.exec_stream(
    inst.name,
    ExecOpts(cmd="sh", args=["-c", "echo hi; echo err >&2"]),
    on_frame=lambda f: print(f.type, f.data or f.error or ""),
)
```

## Create wait modes

Pass `wait` and `timeout` as keyword args (query parameters), same as the Go client:

| Wait | Meaning |
|------|---------|
| `auto` | Daemon chooses (agent for golden images, else SSH) |
| `ssh` | SSH accepts connections |
| `agent` | Guest agent `/health` OK |
| `userdata` | Agent reports userdata finished |

Constants: `WAIT_AUTO`, `WAIT_SSH`, `WAIT_AGENT`, `WAIT_USERDATA`.

## Auth

When the daemon has `api_token` / `auth_token` set, pass the same value as `token` or env `GRAIN_TOKEN`. `GET /healthz` remains unauthenticated.

## Develop / test

```bash
cd sdk/python
pip install -e ".[dev]"
pytest -q
```

## Publishing

Maintainers publish to PyPI via GitHub Actions Trusted Publishing (no API
token in secrets). See [developer/releasing](https://grainvm.com/developer/releasing/)
and workflow [`.github/workflows/publish-python.yml`](../../.github/workflows/publish-python.yml).

## API reference

Daemon OpenAPI: [`api/openapi.yaml`](https://github.com/cxdy/grain/blob/main/api/openapi.yaml). Docs: [grainvm.com/reference/python-sdk](https://grainvm.com/reference/python-sdk/).

## License

Apache-2.0 — same as grain.
