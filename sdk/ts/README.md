# `@cxdy/grain` — TypeScript client

Lightweight TypeScript SDK for the [grain](https://github.com/cxdy/grain) daemon HTTP API. Thin `fetch`-based client with types for VM lifecycle, guest exec, agent health, and stats.

Mirrors the Go client at [`github.com/cxdy/grain/client`](https://pkg.go.dev/github.com/cxdy/grain/client).

## Install

```bash
npm install @cxdy/grain
```

Package: [npmjs.com/package/@cxdy/grain](https://www.npmjs.com/package/@cxdy/grain). Node **18+** (global `fetch`). Optional: `undici` for Unix socket transport.

From a grain checkout (development):

```bash
cd /path/to/grain/sdk/ts
npm install && npm run build
# in your app:
npm install /path/to/grain/sdk/ts
```

## Quick start (TCP)

Start the daemon with TCP API enabled (`api: 127.0.0.1:7474` in `~/.grain/config.yaml`, the default).

```ts
import { GrainClient } from "@cxdy/grain";

const grain = new GrainClient({
  baseURL: "http://127.0.0.1:7474",
  token: process.env.GRAIN_TOKEN, // optional Bearer
});

await grain.health();

const inst = await grain.create({ persistent: false });
console.log("vm", inst.name, inst.status, "ssh", inst.ssh_port);

const result = await grain.exec(inst.name, "uname", ["-a"]);
console.log(result.stdout.trim(), "exit", result.exit_code);

const health = await grain.agentHealth(inst.name);
console.log("agent", health.agent_version, health.hostname);

const stats = await grain.stats(inst.name);
console.log("load1", stats.load1);

await grain.delete(inst.name);
```

## Constructor

```ts
new GrainClient({
  baseURL?: string;   // default http://127.0.0.1:7474
  token?: string;     // Authorization: Bearer <token>
  socketPath?: string; // Unix socket (requires undici)
  fetch?: typeof fetch; // custom fetch (e.g. undici + Agent)
})
```

## Methods

| Method | Daemon route |
|--------|----------------|
| `health()` | `GET /healthz` |
| `info()` | `GET /info` |
| `list()` | `GET /vms` |
| `create(req, opts?)` | `POST /vms` |
| `createStream(req, onEvent?)` | `POST /vms?stream=1` (NDJSON) |
| `get(name)` | `GET /vms/{name}` |
| `delete(name)` | `DELETE /vms/{name}` |
| `start(name)` | `POST /vms/{name}/start` |
| `stop` / `shutdown(name)` | `POST /vms/{name}/shutdown` |
| `pause(name)` | `POST /vms/{name}/pause` |
| `resume(name)` | `POST /vms/{name}/resume` |
| `suspend(name)` | `POST /vms/{name}/suspend` |
| `restore(name)` | `POST /vms/{name}/restore` |
| `exec(name, cmd, args?)` | `POST /vms/{name}/exec?buffered=true` |
| `execStream(name, opts, onFrame)` | `POST /vms/{name}/exec?buffered=false` |
| `agentHealth(name)` | `GET /vms/{name}/agent/health` |
| `stats(name)` | `GET /vms/{name}/stats` |

Errors are thrown as `GrainAPIError` with `status` and optional `body`. Buffered `exec` returns non-zero guest exit codes in `ExecResult.exit_code` without throwing.

### Streamed create

```ts
const inst = await grain.createStream(
  { persistent: false, cpus: 2 },
  (ev) => console.log(ev.phase, ev.message ?? ""),
);
```

### Streamed exec

```ts
const code = await grain.execStream(
  inst.name,
  { cmd: "sh", args: ["-c", "echo hi; echo err >&2"] },
  (frame) => {
    if (frame.type === "stdout") process.stdout.write(frame.data ?? "");
    if (frame.type === "stderr") process.stderr.write(frame.data ?? "");
  },
);
```

## Unix socket (undici)

The CLI talks to `~/.grain/grain.sock`. In Node you can do the same with [undici](https://github.com/nodejs/undici):

```bash
npm install undici
```

**Option A — `socketPath` helper** (loads undici at runtime):

```ts
import { GrainClient } from "@cxdy/grain";
import { homedir } from "node:os";
import { join } from "node:path";

const grain = new GrainClient({
  baseURL: "http://grain",
  socketPath: join(homedir(), ".grain", "grain.sock"),
  token: process.env.GRAIN_TOKEN,
});
```

**Option B — custom `fetch`**:

```ts
import { Agent, fetch as undiciFetch } from "undici";
import { GrainClient } from "@cxdy/grain";
import { homedir } from "node:os";
import { join } from "node:path";

const agent = new Agent({
  connect: { socketPath: join(homedir(), ".grain", "grain.sock") },
});

const grain = new GrainClient({
  baseURL: "http://grain",
  fetch: (input, init) =>
    undiciFetch(input, { ...init, dispatcher: agent }),
});
```

```bash
# equivalent curl
curl --unix-socket ~/.grain/grain.sock http://grain/vms
```

## Auth

When the daemon has `api_token` / `auth_token` set, pass the same value as `token` or `GRAIN_TOKEN`. `GET /healthz` remains unauthenticated.

## Develop

```bash
cd sdk/ts
npm install
npm run build   # tsc → dist/
```

Package is ESM-only (`"type": "module"`) with `.d.ts` types.

## Publishing

Maintainers publish to npm via GitHub Actions Trusted Publishing (OIDC; no
long-lived npm token in secrets). See [developer/releasing](https://grainvm.com/developer/releasing/)
and workflow [`.github/workflows/publish-npm.yml`](../../.github/workflows/publish-npm.yml).

## API reference

Daemon OpenAPI: [`api/openapi.yaml`](https://github.com/cxdy/grain/blob/main/api/openapi.yaml). Go SDK: [`github.com/cxdy/grain/client`](https://pkg.go.dev/github.com/cxdy/grain/client). Docs: [grainvm.com/reference/typescript-sdk](https://grainvm.com/reference/typescript-sdk/).

## License

Apache-2.0 — same as grain.
