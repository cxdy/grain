package proxy

import (
	"net/http"
	"testing"
)

func TestMatchHost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		pattern, host string
		want          bool
	}{
		{"example.com", "example.com", true},
		{"example.com", "EXAMPLE.COM", true},
		{"example.com", "api.example.com", false},
		{"*.example.com", "api.example.com", true},
		{"*.example.com", "a.b.example.com", true},
		{"*.example.com", "example.com", false},
		{"*.example.com", "evil-example.com", false},
		{"", "example.com", false},
		{"example.com", "", false},
	}
	for _, tc := range cases {
		got := MatchHost(tc.pattern, tc.host)
		if got != tc.want {
			t.Errorf("MatchHost(%q,%q)=%v want %v", tc.pattern, tc.host, got, tc.want)
		}
	}
}

func TestRuleMatch(t *testing.T) {
	t.Parallel()
	r := Rule{Host: "api.example.com", Method: "POST", PathPrefix: "/v1/"}
	if !r.Match("POST", "api.example.com", "/v1/chat") {
		t.Fatal("expected match")
	}
	if r.Match("GET", "api.example.com", "/v1/chat") {
		t.Fatal("method mismatch")
	}
	if r.Match("POST", "api.example.com", "/v2/chat") {
		t.Fatal("path mismatch")
	}
	if r.Match("POST", "other.example.com", "/v1/chat") {
		t.Fatal("host mismatch")
	}

	// empty method/path = any
	any := Rule{Host: "example.com"}
	if !any.Match("DELETE", "example.com", "/anything") {
		t.Fatal("any method/path")
	}

	// CONNECT ignores path_prefix
	c := Rule{Host: "example.com", PathPrefix: "/nope"}
	if !c.Match(http.MethodConnect, "example.com", "") {
		t.Fatal("CONNECT should ignore path")
	}
}

func TestFirstMatchDefaultDeny(t *testing.T) {
	t.Parallel()
	rules := []Rule{
		{ID: "1", Host: "allowed.com"},
	}
	if FirstMatch(rules, "GET", "denied.com", "/") != nil {
		t.Fatal("expected deny")
	}
	m := FirstMatch(rules, "GET", "allowed.com", "/")
	if m == nil || m.ID != "1" {
		t.Fatalf("got %+v", m)
	}
}

func TestStripHostPort(t *testing.T) {
	t.Parallel()
	if got := StripHostPort("example.com:443"); got != "example.com" {
		t.Fatal(got)
	}
	if got := StripHostPort("example.com"); got != "example.com" {
		t.Fatal(got)
	}
	if got := StripHostPort("[::1]:443"); got != "::1" {
		t.Fatal(got)
	}
}
