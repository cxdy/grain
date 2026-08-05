package agent

import (
	"io"
	"time"
)

// Version is the grain-agent version string reported by /health.
const Version = "0.3.0"

// DefaultListen is the default guest-side listen address.
// Bind all interfaces so hostfwd / reverse tunnels can reach the agent.
const DefaultListen = ":7475"

// UserdataRanPath is the marker file written after guest userdata completes.
const UserdataRanPath = "/var/lib/grain/userdata-ran"

// ReadinessDir is the guest directory for the readiness protocol files.
// Override in tests with GRAIN_READINESS_DIR.
const ReadinessDir = "/var/lib/grain/readiness"

// Readiness state values (normative for custom images / bootstrap authors).
const (
	ReadinessPending = "pending"
	ReadinessRunning = "running"
	ReadinessReady   = "ready"
	ReadinessFailed  = "failed"
)

// DefaultExecTimeout is the default maximum duration for a buffered or streaming exec.
const DefaultExecTimeout = 5 * time.Minute

// DefaultFileMode is the default permission for created files (binary PUT /cp).
const DefaultFileMode = 0o644

// DefaultDirMode is the default permission for created directories.
const DefaultDirMode = 0o755

// Readiness is the guest bootstrap/readiness contract (see docs readiness protocol).
type Readiness struct {
	// State is pending|running|ready|failed. Empty means no readiness/ files present.
	State     string `json:"state,omitempty"`
	Phase     string `json:"phase,omitempty"`
	Message   string `json:"message,omitempty"`
	ReadyName string `json:"ready_name,omitempty"`
	UpdatedAt string `json:"updated_at,omitempty"` // RFC3339 when set
	Error     string `json:"error,omitempty"`
}

// Health is the response body for GET /health.
type Health struct {
	Hostname     string     `json:"hostname"`
	AgentVersion string     `json:"agent_version"`
	AgentUptime  int64      `json:"agent_uptime_sec"` // seconds
	UserdataRan  bool       `json:"userdata_ran"`
	Readiness    *Readiness `json:"readiness,omitempty"`
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
	Type      string `json:"type"`                // started|stdout|stderr|exit|error
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

// Stats is the response body for GET /stats (guest resource basics).
type Stats struct {
	UptimeSec float64 `json:"uptime_sec"`
	MemTotal  uint64  `json:"mem_total_bytes"`
	MemAvail  uint64  `json:"mem_available_bytes"`
	Load1     float64 `json:"load1"`
	// Optional disk fields (bytes); zero when unavailable.
	DiskTotal uint64 `json:"disk_total_bytes,omitempty"`
	DiskFree  uint64 `json:"disk_free_bytes,omitempty"`
}

// DefaultSecretsDir is the default guest path prefix for materialised secrets.
const DefaultSecretsDir = "/run/grain/secrets"

// MaterializeSecretRequest is the JSON body for POST /secrets/materialize.
type MaterializeSecretRequest struct {
	Name       string  `json:"name"`           // secret name (used for default path)
	DataBase64 string  `json:"data_base64"`    // secret payload
	Path       string  `json:"path,omitempty"` // guest path; default /run/grain/secrets/NAME
	Mode       string  `json:"mode,omitempty"` // octal file mode; default 0600
	UID        *uint32 `json:"uid,omitempty"`
	GID        *uint32 `json:"gid,omitempty"`
}

// MaterializeSecretResponse is returned after writing a secret into the guest.
type MaterializeSecretResponse struct {
	Path string `json:"path"`
	Mode string `json:"mode"`
}

// ShellOpts configures an interactive PTY shell over WebSocket (GET /shell).
type ShellOpts struct {
	// Cols and Rows are the initial terminal size (defaults: 80×24).
	Cols int
	Rows int
	// Shell is the guest shell path (default: login shell of the target user, or /bin/bash).
	Shell string
	// Stdin, Stdout, Stderr are local streams (default: os.Stdin/Stdout/Stderr).
	Stdin  io.Reader
	Stdout io.Writer
	Stderr io.Writer
	// Raw puts the local terminal into raw mode when Stdin is a TTY (default true).
	// Set to false to disable. When Stdin is not a terminal, raw mode is skipped.
	Raw *bool
	// ExtraEnv is KEY=value pairs merged into the guest shell environment
	// (host TERM_PROGRAM / COLORTERM / etc. for TUI keyboard negotiation).
	// Unknown or empty values are ignored by the agent.
	ExtraEnv []string
}

// ShellEnvQueryKeys are query parameters on GET /shell that override guest env.
// Host CLI copies the corresponding process environment into these params so
// guest TUIs (e.g. Grok Build) can detect the outer terminal and enable
// Kitty keyboard protocol / Shift+Enter newlines.
//
// Prefer shellEnvForward in shell_env.go as the source of truth; this list is
// kept for external documentation and tests.
var ShellEnvQueryKeys = []string{
	"term",
	"term_program",
	"term_program_version",
	"colorterm",
	"lc_terminal",
	"lc_terminal_version",
	"term_features",
	"term_session_id",
	"iterm_session_id",
	"iterm_profile",
	"kitty_window_id",
	"kitty_pid",
	"wezterm_pane",
	"wezterm_unix_socket",
	"wt_session",
	"vte_version",
	"lang",
	"lc_all",
	"lc_ctype",
}

// ShellControl is a JSON text WebSocket frame for PTY control messages.
type ShellControl struct {
	Type string `json:"type"` // "resize"
	Cols int    `json:"cols,omitempty"`
	Rows int    `json:"rows,omitempty"`
}
