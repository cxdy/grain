/**
 * @cxdy/grain — TypeScript client for the grain microVM daemon HTTP API.
 *
 * Mirrors the Go package `github.com/cxdy/grain/client` over fetch (TCP by
 * default; Unix socket via optional undici / custom fetch).
 */

export { GrainClient } from "./client.js";
export type { GrainClientOptions } from "./client.js";
export { GrainAPIError } from "./errors.js";
export { Phase, Wait } from "./types.js";
export type {
  AgentHealth,
  APIErrorBody,
  CreateEvent,
  CreateOptions,
  CreateRequest,
  ExecFrame,
  ExecOpts,
  ExecResult,
  Info,
  Instance,
  Mount,
  PortForward,
  SocketForward,
  Stats,
  Status,
  WaitMode,
} from "./types.js";
