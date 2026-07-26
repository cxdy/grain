package agent

import "time"

// Version is the grain-agent version string reported by /health.
const Version = "0.2.0"

// DefaultListen is the default guest-side listen address.
// Bind all interfaces so hostfwd / reverse tunnels can reach the agent.
const DefaultListen = ":7475"

// UserdataRanPath is the marker file written after guest userdata completes.
const UserdataRanPath = "/var/lib/grain/userdata-ran"

// DefaultExecTimeout is the default maximum duration for a buffered or streaming exec.
const DefaultExecTimeout = 5 * time.Minute

// DefaultFileMode is the default permission for created files (binary PUT /cp).
const DefaultFileMode = 0o644

// DefaultDirMode is the default permission for created directories.
const DefaultDirMode = 0o755

// Health is the response body for GET /health.
type Health struct {
	Hostname     string `json:"hostname"`
	AgentVersion string `json:"agent_version"`
	AgentUptime  int64  `json:"agent_uptime_sec"` // seconds
	UserdataRan  bool   `json:"userdata_ran"`
}

// ExecResult is the response body for a buffered POST /exec.
type ExecResult struct {
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	ExitCode int    `json:"exit_code"`
	Error    string `json:"error,omitempty"`
}

// ExecFrame is one NDJSON line for streaming POST /exec?buffered=false.
//
// Order: started (pid) → zero+ stdout/stderr → final exit (exit_code).
// On failure to start: single error frame.
type ExecFrame struct {
	Type      string `json:"type"` // started|stdout|stderr|exit|error
	Timestamp string `json:"timestamp,omitempty"` // RFC3339
	PID       int    `json:"pid,omitempty"`
	Data      string `json:"data,omitempty"` // stdout/stderr chunk
	ExitCode  *int   `json:"exit_code,omitempty"`
	Error     string `json:"error,omitempty"`
	StartedAt string `json:"started_at,omitempty"`
	EndedAt   string `json:"ended_at,omitempty"`
}

// ExecOpts holds optional parameters for buffered or streaming exec.
type ExecOpts struct {
	Cmd  string
	Args []string
	UID  *uint32
	GID  *uint32
	Cwd  string
}

// CPOpts holds optional ownership and mode for PUT /cp (binary).
type CPOpts struct {
	UID  *uint32
	GID  *uint32
	Mode string // file mode e.g. "0644"
}

// FSInfo describes a filesystem entry for /fs/readdir and /fs/stat.
type FSInfo struct {
	Name  string `json:"name"`
	Type  string `json:"type"` // file|directory|symlink
	Size  int64  `json:"size"`
	Mtime int64  `json:"mtime"` // unix sec
	Mode  string `json:"mode"`  // octal e.g. "0644"
}

// MkdirRequest is the JSON body for POST /fs/mkdir.
type MkdirRequest struct {
	Path      string `json:"path"`
	Recursive bool   `json:"recursive"`
	Mode      string `json:"mode"` // optional octal, default 0755
}
