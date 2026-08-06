package activity

import (
	"net/http"
	"strings"
)

// SourceFromRequest maps client identity headers to a short source label.
func SourceFromRequest(r *http.Request) string {
	if r == nil {
		return "api"
	}
	if x := strings.TrimSpace(r.Header.Get("X-Grain-Client")); x != "" {
		return normalizeSource(x)
	}
	ua := strings.ToLower(r.Header.Get("User-Agent"))
	switch {
	case strings.Contains(ua, "grain-cli"):
		return "cli"
	case strings.Contains(ua, "grain-desktop"):
		return "desktop"
	case strings.Contains(ua, "grain-mcp"):
		return "mcp"
	case strings.Contains(ua, "grain-sdk") || strings.Contains(ua, "grain/"):
		return "sdk"
	case ua == "" || ua == "go-http-client/1.1" || strings.HasPrefix(ua, "go-http-client"):
		return "api"
	default:
		return "api"
	}
}

func normalizeSource(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	switch s {
	case "cli", "desktop", "mcp", "sdk", "api":
		return s
	default:
		// allow custom short labels
		if len(s) > 24 {
			s = s[:24]
		}
		return s
	}
}

// ShouldRecord reports whether this request is a control-plane mutation worth logging.
func ShouldRecord(method, path string) bool {
	if method == http.MethodGet || method == http.MethodHead || method == http.MethodOptions {
		// Shell is GET upgrade — still interesting
		if strings.HasSuffix(path, "/shell") || strings.Contains(path, "/shell?") {
			return true
		}
		return false
	}
	switch path {
	case "/healthz", "/info", "/metrics", "/openapi.yaml", "/openapi.json", "/activity":
		return false
	}
	if strings.HasPrefix(path, "/metrics") {
		return false
	}
	return true
}

// Classify maps method+path to a short action and optional target (VM/secret name).
func Classify(method, path string) (action, target string) {
	path = strings.Split(path, "?")[0]
	parts := strings.Split(strings.Trim(path, "/"), "/")
	// empty
	if len(parts) == 0 || parts[0] == "" {
		return strings.ToLower(method) + " " + path, ""
	}

	switch parts[0] {
	case "vms":
		if len(parts) == 1 {
			if method == http.MethodPost {
				return "create", ""
			}
			return method + " /vms", ""
		}
		name := parts[1]
		if len(parts) == 2 {
			if method == http.MethodDelete {
				return "remove", name
			}
			return method + " vm", name
		}
		switch parts[2] {
		case "shutdown":
			return "stop", name
		case "start":
			return "start", name
		case "clone":
			return "clone", name
		case "pause":
			return "pause", name
		case "resume":
			return "resume", name
		case "suspend":
			return "suspend", name
		case "restore":
			return "restore", name
		case "exec":
			return "exec", name
		case "shell":
			return "shell", name
		case "forwards":
			if method == http.MethodPost {
				return "fwd add", name
			}
			return "fwd", name
		case "agent":
			if len(parts) >= 4 && parts[3] == "deploy" {
				return "agent deploy", name
			}
			return "agent", name
		case "cp":
			if method == http.MethodPut {
				return "cp put", name
			}
			return "cp get", name
		case "fs":
			return "fs", name
		case "secrets":
			return "secret inject", name
		default:
			return parts[2], name
		}
	case "pool":
		if len(parts) == 1 {
			return "pool", ""
		}
		return "pool " + parts[1], ""
	case "secrets":
		if len(parts) == 1 {
			if method == http.MethodPost {
				return "secret set", ""
			}
			return "secrets", ""
		}
		if method == http.MethodDelete {
			return "secret delete", parts[1]
		}
		return "secret", parts[1]
	default:
		return strings.ToLower(method) + " " + path, ""
	}
}
