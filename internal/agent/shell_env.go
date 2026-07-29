package agent

import (
	"os"
	"strings"
)

// hostShellEnvKeys are forwarded from the grain CLI host into the guest PTY
// so interactive TUIs can match local keyboard / color behavior.
var hostShellEnvKeys = []string{
	"TERM",
	"TERM_PROGRAM",
	"TERM_PROGRAM_VERSION",
	"COLORTERM",
	"LANG",
	"LC_ALL",
	"LC_CTYPE",
}

// HostShellExtraEnv returns KEY=value pairs from the current process environment
// suitable for ShellOpts.ExtraEnv / GET /shell query params.
func HostShellExtraEnv() []string {
	var out []string
	for _, k := range hostShellEnvKeys {
		if v := strings.TrimSpace(os.Getenv(k)); v != "" {
			out = append(out, k+"="+v)
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
	add := func(envKey, val string) {
		val = strings.TrimSpace(val)
		if val == "" {
			return
		}
		// Reject control characters / newlines in env values.
		if strings.ContainsAny(val, "\x00\n\r") {
			return
		}
		out = append(out, envKey+"="+val)
	}
	add("TERM", q["term"])
	add("TERM_PROGRAM", q["term_program"])
	add("TERM_PROGRAM_VERSION", q["term_program_version"])
	add("COLORTERM", q["colorterm"])
	add("LANG", q["lang"])
	add("LC_ALL", q["lc_all"])
	add("LC_CTYPE", q["lc_ctype"])
	return out
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
