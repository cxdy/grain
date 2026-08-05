import { createRequire } from "node:module";
import { GrainAPIError } from "./errors.js";
import type {
  AgentHealth,
  CreateEvent,
  CreateOptions,
  CreateRequest,
  ExecFrame,
  ExecOpts,
  ExecResult,
  Info,
  Instance,
  Stats,
} from "./types.js";
import { Phase } from "./types.js";

/** Options for {@link GrainClient}. */
export interface GrainClientOptions {
  /**
   * Daemon HTTP base URL (TCP). Default: `http://127.0.0.1:7474`.
   * When using a Unix socket, keep a dummy host (e.g. `http://grain`) and set
   * {@link socketPath} or pass a custom {@link fetch}.
   */
  baseURL?: string;
  /** Bearer token (`Authorization: Bearer …`). Empty when the daemon has no auth. */
  token?: string;
  /**
   * Unix socket path (e.g. `~/.grain/grain.sock`).
   * Uses the optional `undici` package when available. Prefer
   * `baseURL: "http://grain"` with this set for local CLI-style access.
   */
  socketPath?: string;
  /** Custom fetch implementation (e.g. undici with a socket Agent). */
  fetch?: typeof globalThis.fetch;
}

/**
 * Thin fetch-based client for the grain daemon HTTP API.
 *
 * @example
 * ```ts
 * const grain = new GrainClient({ baseURL: "http://127.0.0.1:7474" });
 * await grain.health();
 * const inst = await grain.create({ persistent: false });
 * const out = await grain.exec(inst.name, "uname", "-a");
 * ```
 */
export class GrainClient {
  readonly baseURL: string;
  private token: string;
  private readonly fetchImpl: typeof globalThis.fetch;

  constructor(opts: GrainClientOptions = {}) {
    const base = (opts.baseURL ?? "http://127.0.0.1:7474").replace(/\/+$/, "");
    if (!base) {
      throw new Error("baseURL is required");
    }
    this.baseURL = base;
    this.token = opts.token ?? "";
    this.fetchImpl = opts.fetch ?? createDefaultFetch(opts.socketPath);
  }

  /** Set or clear the Bearer token used on subsequent requests. */
  setToken(token: string): void {
    this.token = token;
  }

  /** Configured Bearer token (may be empty). */
  getToken(): string {
    return this.token;
  }

  // ── system ──────────────────────────────────────────────────────────────

  /** GET /healthz — liveness probe. Throws if not healthy. */
  async health(signal?: AbortSignal): Promise<void> {
    const res = await this.request("GET", "/healthz", { signal, auth: false });
    if (!res.ok) {
      throw new GrainAPIError(res.status, "unhealthy");
    }
  }

  /** GET /info — daemon name + version. */
  async info(signal?: AbortSignal): Promise<Info> {
    return this.json<Info>("GET", "/info", { signal });
  }

  // ── VM lifecycle ────────────────────────────────────────────────────────

  /** GET /vms */
  async list(signal?: AbortSignal): Promise<Instance[]> {
    return this.json<Instance[]>("GET", "/vms", { signal });
  }

  /**
   * POST /vms — blocking create until ready (default wait=true).
   * Use {@link createStream} for NDJSON progress.
   */
  async create(
    req: CreateRequest = {},
    opts: CreateOptions = {},
    signal?: AbortSignal,
  ): Promise<Instance> {
    const q = new URLSearchParams();
    if (typeof opts.wait === "string") q.set("wait", opts.wait);
    else if (opts.wait === false) q.set("wait", "false");
    if (opts.timeout) q.set("timeout", opts.timeout);
    const path = q.size ? `/vms?${q}` : "/vms";
    return this.json<Instance>("POST", path, {
      body: req,
      signal,
    });
  }

  /**
   * POST /vms?stream=1 — NDJSON create progress.
   * Calls `onEvent` for each line; returns the instance from the ready event.
   */
  async createStream(
    req: CreateRequest = {},
    onEvent?: (ev: CreateEvent) => void,
    signal?: AbortSignal,
  ): Promise<Instance> {
    const res = await this.request("POST", "/vms?stream=1", {
      body: req,
      signal,
      headers: { Accept: "application/x-ndjson" },
    });
    if (!res.ok) {
      throw await this.errorFromResponse(res);
    }
    if (!res.body) {
      throw new Error("create stream: empty body");
    }

    let inst: Instance | undefined;
    let streamErr: Error | undefined;

    for await (const line of readLines(res.body)) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      let ev: CreateEvent;
      try {
        ev = JSON.parse(trimmed) as CreateEvent;
      } catch {
        throw new Error(`decode create event: ${trimmed}`);
      }
      onEvent?.(ev);
      if (ev.phase === Phase.Ready) {
        if (ev.instance) {
          inst = ev.instance;
        } else if (ev.name) {
          inst = {
            name: ev.name,
            status: "running",
            persistent: false,
            cpus: 0,
            memory_mb: 0,
            disk_gb: 0,
            image: "",
            created_at: new Date().toISOString(),
            ssh_port: ev.ssh_port,
          };
        }
      } else if (ev.phase === Phase.Error) {
        streamErr = new Error(ev.error || ev.message || "create failed");
      }
    }

    if (streamErr) throw streamErr;
    if (!inst) throw new Error("create stream ended without ready event");
    return inst;
  }

  /** GET /vms/{name} */
  async get(name: string, signal?: AbortSignal): Promise<Instance> {
    return this.json<Instance>("GET", `/vms/${enc(name)}`, { signal });
  }

  /** DELETE /vms/{name} */
  async delete(name: string, signal?: AbortSignal): Promise<void> {
    await this.empty("DELETE", `/vms/${enc(name)}`, { signal });
  }

  /** POST /vms/{name}/start */
  async start(name: string, signal?: AbortSignal): Promise<Instance> {
    return this.json<Instance>("POST", `/vms/${enc(name)}/start`, { signal });
  }

  /** POST /vms/{name}/shutdown (alias: stop). */
  async stop(name: string, signal?: AbortSignal): Promise<void> {
    return this.shutdown(name, signal);
  }

  /** POST /vms/{name}/shutdown */
  async shutdown(name: string, signal?: AbortSignal): Promise<void> {
    await this.empty("POST", `/vms/${enc(name)}/shutdown`, { signal });
  }

  /** POST /vms/{name}/pause — freeze guest vCPUs via QMP. */
  async pause(name: string, signal?: AbortSignal): Promise<void> {
    await this.empty("POST", `/vms/${enc(name)}/pause`, { signal });
  }

  /** POST /vms/{name}/resume */
  async resume(name: string, signal?: AbortSignal): Promise<void> {
    await this.empty("POST", `/vms/${enc(name)}/resume`, { signal });
  }

  /**
   * POST /vms/{name}/suspend — stop a persistent VM and free host RAM.
   * Differs from pause (which freezes vCPUs while QEMU stays running).
   */
  async suspend(name: string, signal?: AbortSignal): Promise<void> {
    await this.empty("POST", `/vms/${enc(name)}/suspend`, { signal });
  }

  /** POST /vms/{name}/restore — boot a suspended VM. */
  async restore(name: string, signal?: AbortSignal): Promise<Instance> {
    return this.json<Instance>("POST", `/vms/${enc(name)}/restore`, { signal });
  }

  // ── agent ───────────────────────────────────────────────────────────────

  /**
   * POST /vms/{name}/exec?buffered=true — run a command; returns stdout/stderr.
   * Non-zero remote exit codes are returned in {@link ExecResult} without throwing.
   */
  async exec(
    name: string,
    cmd: string,
    args: string[] = [],
    opts: Omit<ExecOpts, "cmd" | "args"> = {},
    signal?: AbortSignal,
  ): Promise<ExecResult> {
    if (!cmd) throw new Error("cmd is required");
    const q = new URLSearchParams();
    q.set("cmd", cmd);
    for (const a of args) q.append("args", a);
    q.set("buffered", "true");
    if (opts.uid !== undefined) q.set("uid", String(opts.uid));
    if (opts.gid !== undefined) q.set("gid", String(opts.gid));
    if (opts.cwd) q.set("cwd", opts.cwd);

    const res = await this.request(
      "POST",
      `/vms/${enc(name)}/exec?${q}`,
      { signal },
    );
    const text = await res.text();
    if (!res.ok) {
      let msg = text.trim() || `status ${res.status}`;
      try {
        const e = JSON.parse(text) as { error?: string };
        if (e.error) msg = e.error;
      } catch {
        /* keep msg */
      }
      throw new GrainAPIError(res.status, msg, text);
    }
    return JSON.parse(text) as ExecResult;
  }

  /**
   * POST /vms/{name}/exec?buffered=false — NDJSON stream of {@link ExecFrame}.
   * Returns the final exit code from the exit frame.
   */
  async execStream(
    name: string,
    opts: ExecOpts,
    onFrame: (frame: ExecFrame) => void | Promise<void>,
    signal?: AbortSignal,
  ): Promise<number> {
    if (!opts.cmd) throw new Error("cmd is required");
    if (!onFrame) throw new Error("onFrame is required");

    const q = new URLSearchParams();
    q.set("cmd", opts.cmd);
    for (const a of opts.args ?? []) q.append("args", a);
    q.set("buffered", "false");
    if (opts.uid !== undefined) q.set("uid", String(opts.uid));
    if (opts.gid !== undefined) q.set("gid", String(opts.gid));
    if (opts.cwd) q.set("cwd", opts.cwd);

    const res = await this.request(
      "POST",
      `/vms/${enc(name)}/exec?${q}`,
      { signal },
    );
    if (!res.ok) throw await this.errorFromResponse(res);
    if (!res.body) throw new Error("exec stream: empty body");

    let gotExit = false;
    let code = -1;

    for await (const line of readLines(res.body)) {
      const trimmed = line.trim();
      if (!trimmed) continue;
      let frame: ExecFrame;
      try {
        frame = JSON.parse(trimmed) as ExecFrame;
      } catch {
        throw new Error(`exec stream frame decode: ${trimmed}`);
      }
      await onFrame(frame);
      if (frame.type === "exit") {
        gotExit = true;
        if (frame.exit_code !== undefined) code = frame.exit_code;
      } else if (frame.type === "error") {
        throw new Error(frame.error || "exec stream error");
      }
    }

    if (!gotExit) throw new Error("exec stream ended without exit frame");
    return code;
  }

  /** GET /vms/{name}/agent/health */
  async agentHealth(name: string, signal?: AbortSignal): Promise<AgentHealth> {
    return this.json<AgentHealth>("GET", `/vms/${enc(name)}/agent/health`, {
      signal,
    });
  }

  /** GET /vms/{name}/stats */
  async stats(name: string, signal?: AbortSignal): Promise<Stats> {
    return this.json<Stats>("GET", `/vms/${enc(name)}/stats`, { signal });
  }

  // ── HTTP helpers ────────────────────────────────────────────────────────

  private async request(
    method: string,
    path: string,
    init: {
      body?: unknown;
      signal?: AbortSignal;
      headers?: Record<string, string>;
      auth?: boolean;
    } = {},
  ): Promise<Response> {
    const headers: Record<string, string> = { ...(init.headers ?? {}) };
    if (init.auth !== false && this.token) {
      headers["Authorization"] = `Bearer ${this.token}`;
    }
    let body: string | undefined;
    if (init.body !== undefined) {
      headers["Content-Type"] = headers["Content-Type"] ?? "application/json";
      body = JSON.stringify(init.body);
    }
    return this.fetchImpl(`${this.baseURL}${path}`, {
      method,
      headers,
      body,
      signal: init.signal,
    });
  }

  private async json<T>(
    method: string,
    path: string,
    init: {
      body?: unknown;
      signal?: AbortSignal;
      headers?: Record<string, string>;
    } = {},
  ): Promise<T> {
    const res = await this.request(method, path, init);
    if (!res.ok) throw await this.errorFromResponse(res);
    return (await res.json()) as T;
  }

  private async empty(
    method: string,
    path: string,
    init: { signal?: AbortSignal } = {},
  ): Promise<void> {
    const res = await this.request(method, path, init);
    if (!res.ok) throw await this.errorFromResponse(res);
  }

  private async errorFromResponse(res: Response): Promise<GrainAPIError> {
    const text = await res.text().catch(() => "");
    let msg = `status ${res.status}`;
    if (text) {
      try {
        const e = JSON.parse(text) as { error?: string };
        if (e.error) msg = e.error;
        else msg = text.trim() || msg;
      } catch {
        msg = text.trim() || msg;
      }
    }
    return new GrainAPIError(res.status, msg, text || undefined);
  }
}

function enc(name: string): string {
  return encodeURIComponent(name);
}

/**
 * Build a default fetch. When `socketPath` is set, load undici's Agent with
 * `connect.socketPath` (Node). Otherwise use global fetch (TCP).
 */
function createDefaultFetch(
  socketPath?: string,
): typeof globalThis.fetch {
  if (!socketPath) {
    if (typeof globalThis.fetch !== "function") {
      throw new Error(
        "global fetch is unavailable; pass GrainClientOptions.fetch or use Node 18+",
      );
    }
    return globalThis.fetch.bind(globalThis);
  }

  try {
    const require = createRequire(import.meta.url);
    // Optional dependency — see package.json optionalDependencies / README.
    // eslint-disable-next-line @typescript-eslint/no-explicit-any
    const undici = require("undici") as any;
    const agent = new undici.Agent({ connect: { socketPath } });
    return ((input: RequestInfo | URL, init?: RequestInit) =>
      undici.fetch(input, {
        ...init,
        dispatcher: agent,
      })) as typeof globalThis.fetch;
  } catch (err) {
    const hint = err instanceof Error ? err.message : String(err);
    throw new Error(
      `socketPath requires the optional "undici" package (npm i undici): ${hint}`,
    );
  }
}

/** Async-iterate text lines from a fetch body stream. */
async function* readLines(
  body: ReadableStream<Uint8Array>,
): AsyncGenerator<string> {
  const reader = body.getReader();
  const decoder = new TextDecoder();
  let buffer = "";
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      buffer += decoder.decode(value, { stream: true });
      let idx: number;
      while ((idx = buffer.indexOf("\n")) >= 0) {
        const line = buffer.slice(0, idx);
        buffer = buffer.slice(idx + 1);
        yield line;
      }
    }
    buffer += decoder.decode();
    if (buffer.length) yield buffer;
  } finally {
    reader.releaseLock();
  }
}
