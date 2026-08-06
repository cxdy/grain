package desktop

import (
	"fmt"
	"sort"
	"strings"
)

// MultiRunResult is one host's outcome from multi-select Run (bulk exec).
type MultiRunResult struct {
	Name     string `json:"name"`
	ExitCode int    `json:"exit_code,omitempty"`
	Stdout   string `json:"stdout,omitempty"`
	Stderr   string `json:"stderr,omitempty"`
	Error    string `json:"error,omitempty"`
	Line     string `json:"line,omitempty"`
}

// MultiRunFailed reports whether a result should be re-run (error or non-zero exit).
func MultiRunFailed(r MultiRunResult) bool {
	if strings.TrimSpace(r.Error) != "" {
		return true
	}
	if r.ExitCode != 0 {
		return true
	}
	return false
}

// PartitionMultiRunFailed returns names that failed (stable sorted unique).
func PartitionMultiRunFailed(results []MultiRunResult) (failed, succeeded []string) {
	seenF, seenOK := map[string]struct{}{}, map[string]struct{}{}
	for _, r := range results {
		name := strings.TrimSpace(r.Name)
		if name == "" {
			continue
		}
		if MultiRunFailed(r) {
			if _, exists := seenF[name]; !exists {
				seenF[name] = struct{}{}
				failed = append(failed, name)
			}
		} else {
			if _, exists := seenOK[name]; !exists {
				seenOK[name] = struct{}{}
				succeeded = append(succeeded, name)
			}
		}
	}
	sort.Strings(failed)
	sort.Strings(succeeded)
	return failed, succeeded
}

// FormatMultiRunExport builds a copy/export payload for all multi-Run results.
// Includes stdout and stderr separately when present; failed hosts are marked.
func FormatMultiRunExport(command string, results []MultiRunResult) string {
	var b strings.Builder
	cmd := strings.TrimSpace(command)
	if cmd != "" {
		b.WriteString("$ ")
		b.WriteString(cmd)
		b.WriteByte('\n')
		b.WriteByte('\n')
	}
	// Stable order by name
	cp := append([]MultiRunResult(nil), results...)
	sort.Slice(cp, func(i, j int) bool {
		return cp[i].Name < cp[j].Name
	})
	for _, r := range cp {
		name := r.Name
		if name == "" {
			name = "?"
		}
		status := "ok"
		if MultiRunFailed(r) {
			status = "FAILED"
		}
		fmt.Fprintf(&b, "=== %s (%s) ===\n", name, status)
		if e := strings.TrimSpace(r.Error); e != "" {
			b.WriteString("error: ")
			b.WriteString(e)
			b.WriteByte('\n')
		}
		if r.ExitCode != 0 {
			fmt.Fprintf(&b, "exit_code: %d\n", r.ExitCode)
		}
		out := strings.TrimRight(r.Stdout, "\r\n")
		errOut := strings.TrimRight(r.Stderr, "\r\n")
		if out != "" {
			b.WriteString("--- stdout ---\n")
			b.WriteString(out)
			b.WriteByte('\n')
		}
		if errOut != "" {
			b.WriteString("--- stderr ---\n")
			b.WriteString(errOut)
			b.WriteByte('\n')
		}
		if out == "" && errOut == "" && strings.TrimSpace(r.Error) == "" {
			// Fall back to combined line if that is all we have.
			if ln := strings.TrimSpace(r.Line); ln != "" {
				b.WriteString(ln)
				b.WriteByte('\n')
			} else {
				fmt.Fprintf(&b, "(exit %d)\n", r.ExitCode)
			}
		}
		b.WriteByte('\n')
	}
	return b.String()
}

// FormatMultiRunStderrBlock highlights stderr/error for UI presentation.
func FormatMultiRunStderrBlock(r MultiRunResult) string {
	var parts []string
	if e := strings.TrimSpace(r.Error); e != "" {
		parts = append(parts, "error: "+e)
	}
	if s := strings.TrimRight(r.Stderr, "\r\n"); s != "" {
		parts = append(parts, s)
	}
	if r.ExitCode != 0 && len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("exit %d", r.ExitCode))
	}
	return strings.Join(parts, "\n")
}
