---
title: "TypeScript SDK (@cxdy/grain)"
description: Node-friendly fetch client for the grain daemon (@cxdy/grain under sdk/ts).
section: reference
keywords:
  - TypeScript
  - SDK
  - Node
  - npm
  - "@cxdy/grain"
---

npm: **[`@cxdy/grain`](https://www.npmjs.com/package/@cxdy/grain)**.  
Source: [`sdk/ts`](https://github.com/cxdy/grain/tree/main/sdk/ts).

## Install

```bash
npm install @cxdy/grain
```

Requires **Node 18+** (global `fetch`). Optional **`undici`** for Unix socket transport.

## Quick start (TCP)

Ensure the daemon is up with TCP API enabled (`api: 127.0.0.1:7474` is the default config).

```ts
import { GrainClient } from "@cxdy/grain";

const grain = new GrainClient({
  baseURL: "http://127.0.0.1:7474",
  token: process.env.GRAIN_TOKEN, // optional Bearer
});

await grain.health();

const inst = await grain.create({
  persistent: false,
  wait: "agent",
  timeout: "3m",
});

console.log(inst.name, inst.status, inst.ssh_port);

const result = await grain.exec(inst.name, "uname", ["-a"]);
console.log(result.stdout.trim(), "exit", result.exit_code);

const agent = await grain.agentHealth(inst.name);
console.log(agent.agent_version, agent.hostname);

const stats = await grain.stats(inst.name);
console.log("load1", stats.load1);

await grain.delete(inst.name);
```

## Constructor options

```ts
new GrainClient({
  baseURL: string;       // e.g. http://127.0.0.1:7474
  token?: string;        // Authorization: Bearer
  socketPath?: string;   // optional unix socket (uses undici)
  fetch?: typeof fetch;  // inject custom fetch
})
```

### Unix socket

```ts
const grain = new GrainClient({
  baseURL: "http://grain",
  socketPath: `${process.env.HOME}/.grain/grain.sock`,
});
```

## Methods (overview)

| Area | Methods |
|------|---------|
| System | `health()`, `info()` |
| VMs | `list`, `create`, `createStream`, `get`, `delete` |
| Lifecycle | `start`, `stop` / `shutdown`, `pause`, `resume`, `suspend`, `restore` |
| Agent | `exec`, `execStream`, `agentHealth`, `stats` |

### Create wait modes

Pass `wait` and `timeout` on create options (mapped to query string), same semantics as the Go client: `auto` | `ssh` | `agent` | `userdata`.

### Streaming create

```ts
const inst = await grain.createStream(
  { persistent: false, wait: "ssh" },
  (ev) => console.log(ev.phase, ev.message)
);
```

### Streaming exec

```ts
const code = await grain.execStream(inst.name, {
  cmd: "bash",
  args: ["-lc", "echo hello"],
}, (frame) => {
  if (frame.type === "stdout") process.stdout.write(frame.data ?? "");
});
```

## Errors

Failed responses throw `GrainAPIError` (or equivalent) with HTTP status and message body when available.

## Relationship to the Go SDK

| | Go | TypeScript |
|--|-----|------------|
| Module | `github.com/cxdy/grain/client` | `@cxdy/grain` (`sdk/ts`) |
| Transport | `net/http` + unix dialer | `fetch` + optional undici socket |
| Surface | Broader (cp/fs/secrets helpers) | Lifecycle + exec/health/stats first |

For full file and secrets helpers, use the CLI, curl, or the Go client until those methods are added to the TS package.

## See also

- [HTTP API](../api/)  
- [Go SDK](../go-sdk/)  
- Source README: [`sdk/ts/README.md`](https://github.com/cxdy/grain/blob/main/sdk/ts/README.md)  
