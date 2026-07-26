package agent

import "time"

// Version is the grain-agent version string reported by /health.
const Version = "0.1.0"

// DefaultListen is the default guest-side listen address.
// Bind all interfaces so hostfwd / reverse tunnels can reach the agent.
const DefaultListen = ":7475"

// UserdataRanPath is the marker file written after guest userdata completes.
const UserdataRanPath = "/var/lib/grain/userdata-ran"

// DefaultExecTimeout is the default maximum duration for a buffered exec.
const DefaultExecTimeout = 5 * time.Minute

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
