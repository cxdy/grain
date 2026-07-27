package proxy

import (
	"net"
	"net/http"
	"strings"
)

// Rule is an allowlist entry for outbound proxy traffic.
// Empty Method or PathPrefix means "any". Host is required for a useful rule.
type Rule struct {
	ID         string `json:"id"`
	Host       string `json:"host"`                  // exact or suffix: example.com, *.example.com
	Method     string `json:"method,omitempty"`      // empty = any
	PathPrefix string `json:"path_prefix,omitempty"` // empty = any (HTTP only; CONNECT has no path)
	SecretName string `json:"secret_name,omitempty"` // optional host secret to inject as Authorization
}

// MatchHost reports whether host (hostname without port) matches the rule host pattern.
// Patterns:
//   - "example.com" exact (case-insensitive)
//   - "*.example.com" suffix match for subdomains (not the bare domain itself)
//   - ".example.com" or "example.com" with leading "*." variants
//
// Also accepts "example.com:443" style by stripping port first in MatchRequest.
func MatchHost(pattern, host string) bool {
	pattern = strings.ToLower(strings.TrimSpace(pattern))
	host = strings.ToLower(strings.TrimSpace(host))
	if pattern == "" || host == "" {
		return false
	}
	// Strip brackets from IPv6 host for comparison consistency.
	host = strings.TrimPrefix(host, "[")
	host = strings.TrimSuffix(host, "]")

	if strings.HasPrefix(pattern, "*.") {
		suffix := pattern[1:] // ".example.com"
		base := pattern[2:]   // "example.com"
		if host == base {
			// bare domain does not match *.example.com
			return false
		}
		return strings.HasSuffix(host, suffix)
	}
	return host == pattern
}

// Match reports whether the request target matches this rule.
// For CONNECT, path is ignored (only host + optional method CONNECT).
func (r Rule) Match(method, host, path string) bool {
	if r.Host == "" {
		return false
	}
	if !MatchHost(r.Host, host) {
		return false
	}
	if r.Method != "" && !strings.EqualFold(r.Method, method) {
		return false
	}
	// CONNECT has no meaningful path for allowlist purposes.
	if strings.EqualFold(method, http.MethodConnect) {
		return true
	}
	if r.PathPrefix != "" {
		if path == "" {
			path = "/"
		}
		if !strings.HasPrefix(path, r.PathPrefix) {
			return false
		}
	}
	return true
}

// StripHostPort returns hostname without port (handles host:port and [ipv6]:port).
func StripHostPort(hostport string) string {
	hostport = strings.TrimSpace(hostport)
	if hostport == "" {
		return ""
	}
	h, _, err := net.SplitHostPort(hostport)
	if err == nil {
		return h
	}
	// No port, or bare IPv6 without brackets — return as-is (trim brackets).
	return strings.Trim(hostport, "[]")
}

// FirstMatch returns the first matching rule or nil.
func FirstMatch(rules []Rule, method, host, path string) *Rule {
	host = StripHostPort(host)
	for i := range rules {
		if rules[i].Match(method, host, path) {
			r := rules[i]
			return &r
		}
	}
	return nil
}
