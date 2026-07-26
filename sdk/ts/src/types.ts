/** Status of a sandbox VM. */
export type Status =
  | "creating"
  | "running"
  | "paused"
  | "suspended"
  | "stopped"
  | "error";

/** Host → guest port map (SLIRP hostfwd). Host port 0 = allocate. */
export interface PortForward {
  host_port: number;
  guest_port: number;
  proto?: string;
}

/** Host directory shared into the guest via virtio-9p. */
export interface Mount {
  host: string;
  guest: string;
  tag?: string;
}

/** Host Unix socket → guest Unix socket via SSH streamlocal. */
export interface SocketForward {
  host_path: string;
  guest_path: string;
  pid?: number;
}

/** Managed microVM as returned by the daemon API. */
export interface Instance {
  name: string;
  status: Status;
  persistent: boolean;
  cpus: number;
  memory_mb: number;
  disk_gb: number;
  image: string;
  ip?: string;
  ssh_port?: number;
  agent_port?: number;
  /** Guest virtio-vsock context ID (0 = TCP hostfwd only). */
  agent_cid?: number;
  forwards?: PortForward[];
  mounts?: Mount[];
  socket_forwards?: SocketForward[];
  tags?: Record<string, string>;
  created_at: string;
  suspended_at?: string;
  error?: string;
  disk_path?: string;
  pid?: number;
}

/** JSON body for POST /vms. */
export interface CreateRequest {
  name?: string;
  persistent?: boolean;
  cpus?: number;
  memory_mb?: number;
  disk_gb?: number;
  image?: string;
  tags?: Record<string, string>;
  userdata?: string;
  forwards?: PortForward[];
  mounts?: Mount[];
  socket_forwards?: SocketForward[];
}

/** Optional query params for create. */
export interface CreateOptions {
  /** When not streaming, wait until ready (default true). */
  wait?: boolean;
  /** Go duration string, e.g. "3m", "90s". */
  timeout?: string;
}

/** Guest resource basics from GET /vms/{name}/stats. */
export interface Stats {
  uptime_sec: number;
  mem_total_bytes: number;
  mem_available_bytes: number;
  load1: number;
  disk_total_bytes?: number;
  disk_free_bytes?: number;
}

/** One NDJSON progress line during streamed create. */
export interface CreateEvent {
  phase: string;
  message?: string;
  name?: string;
  error?: string;
  ssh_port?: number;
  instance?: Instance;
}

/** Create phase constants (match daemon). */
export const Phase = {
  Image: "image",
  Disk: "disk",
  Seed: "seed",
  QEMU: "qemu",
  WaitSSH: "wait_ssh",
  WaitAgent: "wait_agent",
  Ready: "ready",
  Error: "error",
} as const;

/** Guest grain-agent GET /health (proxied via daemon). */
export interface AgentHealth {
  hostname: string;
  agent_version: string;
  agent_uptime_sec: number;
  userdata_ran: boolean;
}

/** Buffered POST /vms/{name}/exec response. */
export interface ExecResult {
  stdout: string;
  stderr: string;
  exit_code: number;
  error?: string;
}

/** One NDJSON line for streaming exec (buffered=false). */
export interface ExecFrame {
  type: string;
  timestamp?: string;
  pid?: number;
  data?: string;
  exit_code?: number;
  error?: string;
  started_at?: string;
  ended_at?: string;
}

/** Optional parameters for buffered or streaming exec. */
export interface ExecOpts {
  cmd: string;
  args?: string[];
  uid?: number;
  gid?: number;
  cwd?: string;
}

/** Daemon identity from GET /info. */
export interface Info {
  name: string;
  version: string;
  [key: string]: string;
}

/** API error payload `{ "error": "..." }`. */
export interface APIErrorBody {
  error: string;
}
