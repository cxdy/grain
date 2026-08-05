package agent

import (
	"net/url"
	"os"
	"strings"
)

// shellEnvForward maps host process env vars → GET /shell query parameter names.
// Kept in one table so CLI encode and agent decode cannot drift.
//
// Includes terminal identity keys used by TUIs (Grok Build, etc.) and multiplexers
// to enable Kitty keyboard protocol / modified Enter (Shift+Enter newlines), not
// just the generic TERM=xterm-256color default.
var shellEnvForward = []struct {
	Env   string
	Query string
}{
	{"TERM", "term"},
	{"TERM_PROGRAM", "term_program"},
	{"TERM_PROGRAM_VERSION", "term_program_version"},
	{"COLORTERM", "colorterm"},
	// iTerm2 capability negotiation (Shift+Enter, extended keys).
	{"LC_TERMINAL", "lc_terminal"},
	{"LC_TERMINAL_VERSION", "lc_terminal_version"},
	{"TERM_FEATURES", "term_features"},
	{"TERM_SESSION_ID", "term_session_id"},
	{"ITERM_SESSION_ID", "iterm_session_id"},
	{"ITERM_PROFILE", "iterm_profile"},
	// Kitty / WezTerm / Windows Terminal / VTE detection.
	{"KITTY_WINDOW_ID", "kitty_window_id"},
	{"KITTY_PID", "kitty_pid"},
	{"WEZTERM_PANE", "wezterm_pane"},
	{"WEZTERM_UNIX_SOCKET", "wezterm_unix_socket"},
	{"WT_SESSION", "wt_session"},
	{"VTE_VERSION", "vte_version"},
	// Locale (affects UTF-8 and some TUI input).
	{"LANG", "lang"},
	{"LC_ALL", "lc_all"},
	{"LC_CTYPE", "lc_ctype"},
}

// HostShellExtraEnv returns KEY=value pairs from the current process environment
// suitable for ShellOpts.ExtraEnv / GET /shell query params.
func HostShellExtraEnv() []string {
	var out []string
	for _, e := range shellEnvForward {
		if v := strings.TrimSpace(os.Getenv(e.Env)); v != "" {
			out = append(out, e.Env+"="+v)
		}
	}
	return out
}

// shellEnvFromQuery builds extra env KEY=value pairs from /shell query values.
func shellEnvFromQuery(q map[string]string) []string {
	if len(q) == 0 {
		return nil
	}
	var out []string
	for _, e := range shellEnvForward {
		val := strings.TrimSpace(q[e.Query])
		if val == "" {
			continue
		}
		// Reject control characters / newlines in env values.
		if strings.ContainsAny(val, "\x00\n\r") {
			continue
		}
		// Cap length — query strings and env slots are not free-form blobs.
		if len(val) > 512 {
			val = val[:512]
		}
		out = append(out, e.Env+"="+val)
	}
	return out
}

// shellEnvToQuery sets URL query values from KEY=value ExtraEnv pairs.
func shellEnvToQuery(q url.Values, env []string) {
	if len(env) == 0 {
		return
	}
	byEnv := make(map[string]string, len(shellEnvForward))
	for _, e := range shellEnvForward {
		byEnv[e.Env] = e.Query
	}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || v == "" {
			continue
		}
		if query, ok := byEnv[k]; ok {
			q.Set(query, v)
			continue
		}
		// Also accept case-insensitive env names from callers.
		if query, ok := byEnv[strings.ToUpper(k)]; ok {
			q.Set(query, v)
		}
	}
}

// mergeShellEnv applies override KEY=value pairs onto base env (later wins).
func mergeShellEnv(base []string, override []string) []string {
	if len(override) == 0 {
		return base
	}
	idx := make(map[string]int, len(base))
	out := append([]string(nil), base...)
	for i, kv := range out {
		k, _, ok := strings.Cut(kv, "=")
		if ok {
			idx[k] = i
		}
	}
	for _, kv := range override {
		k, _, ok := strings.Cut(kv, "=")
		if !ok || k == "" {
			continue
		}
		if i, exists := idx[k]; exists {
			out[i] = kv
		} else {
			idx[k] = len(out)
			out = append(out, kv)
		}
	}
	return out
}
