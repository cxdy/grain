package agent

import (
	"os"
	"path/filepath"
	"strings"
)

// readinessDir returns the guest readiness directory (env override for tests).
func readinessDir() string {
	if d := strings.TrimSpace(os.Getenv("GRAIN_READINESS_DIR")); d != "" {
		return d
	}
	return ReadinessDir
}

// LoadReadiness reads /var/lib/grain/readiness/* (or GRAIN_READINESS_DIR).
// Returns nil when the directory or state file is absent (stock images).
func LoadReadiness() *Readiness {
	dir := readinessDir()
	statePath := filepath.Join(dir, "state")
	b, err := os.ReadFile(statePath)
	if err != nil {
		return nil
	}
	state := strings.ToLower(strings.TrimSpace(string(b)))
	if state == "" {
		return nil
	}
	// Normalize unknown values but still surface them.
	switch state {
	case ReadinessPending, ReadinessRunning, ReadinessReady, ReadinessFailed:
	default:
		// keep as-is (author typo still visible)
	}
	r := &Readiness{
		State:     state,
		Phase:     readTrim(filepath.Join(dir, "phase")),
		Message:   readTrim(filepath.Join(dir, "message")),
		ReadyName: readTrim(filepath.Join(dir, "ready_name")),
		UpdatedAt: readTrim(filepath.Join(dir, "updated_at")),
		Error:     readTrim(filepath.Join(dir, "error")),
	}
	return r
}

func readTrim(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// StatusLine is a short human summary for CLI / create progress.
func (r *Readiness) StatusLine() string {
	if r == nil || r.State == "" {
		return ""
	}
	var b strings.Builder
	b.WriteString(r.State)
	if r.Phase != "" {
		b.WriteString(" ")
		b.WriteString(r.Phase)
	}
	if r.Message != "" {
		b.WriteString(" — ")
		b.WriteString(r.Message)
	} else if r.State == ReadinessFailed && r.Error != "" {
		b.WriteString(" — ")
		b.WriteString(r.Error)
	}
	return b.String()
}
