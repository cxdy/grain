package activity_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/activity"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		m, p, act, tgt string
	}{
		{"POST", "/vms", "create", ""},
		{"GET", "/vms", "GET /vms", ""},
		{"DELETE", "/vms/tiny-1", "remove", "tiny-1"},
		{"GET", "/vms/tiny-1", "GET vm", "tiny-1"},
		{"PATCH", "/vms/lab", "PATCH vm", "lab"},
		{"POST", "/vms/lab/shutdown", "stop", "lab"},
		{"POST", "/vms/lab/start", "start", "lab"},
		{"POST", "/vms/lab/clone", "clone", "lab"},
		{"POST", "/vms/lab/pause", "pause", "lab"},
		{"POST", "/vms/lab/resume", "resume", "lab"},
		{"POST", "/vms/lab/suspend", "suspend", "lab"},
		{"POST", "/vms/lab/restore", "restore", "lab"},
		{"POST", "/vms/lab/exec", "exec", "lab"},
		{"GET", "/vms/lab/shell", "shell", "lab"},
		{"GET", "/vms/lab/shell?cols=80", "shell", "lab"},
		{"POST", "/vms/lab/forwards", "fwd add", "lab"},
		{"DELETE", "/vms/lab/forwards", "fwd", "lab"},
		{"POST", "/vms/lab/agent/deploy", "agent deploy", "lab"},
		{"GET", "/vms/lab/agent", "agent", "lab"},
		{"PUT", "/vms/lab/cp", "cp put", "lab"},
		{"GET", "/vms/lab/cp", "cp get", "lab"},
		{"GET", "/vms/lab/fs", "fs", "lab"},
		{"POST", "/vms/lab/secrets", "secret inject", "lab"},
		{"POST", "/vms/lab/custom-op", "custom-op", "lab"},
		{"POST", "/pool", "pool", ""},
		{"POST", "/pool/claim", "pool claim", ""},
		{"GET", "/pool/status", "pool status", ""},
		{"POST", "/secrets", "secret set", ""},
		{"GET", "/secrets", "secrets", ""},
		{"DELETE", "/secrets/db-pass", "secret delete", "db-pass"},
		{"GET", "/secrets/db-pass", "secret", "db-pass"},
		{"POST", "/other/thing", "post /other/thing", ""},
		{"GET", "/", "get /", ""},
		{"GET", "", "get ", ""},
	}
	for _, tc := range cases {
		a, tgt := activity.Classify(tc.m, tc.p)
		if a != tc.act || tgt != tc.tgt {
			t.Fatalf("%s %s: got %q %q want %q %q", tc.m, tc.p, a, tgt, tc.act, tc.tgt)
		}
	}
}

func TestSourceFromRequest(t *testing.T) {
	t.Parallel()
	if activity.SourceFromRequest(nil) != "api" {
		t.Fatal("nil request")
	}

	r, _ := http.NewRequest(http.MethodPost, "/vms", nil)
	r.Header.Set("User-Agent", "grain-cli/dev")
	if activity.SourceFromRequest(r) != "cli" {
		t.Fatal(activity.SourceFromRequest(r))
	}
	r.Header.Set("X-Grain-Client", "desktop")
	if activity.SourceFromRequest(r) != "desktop" {
		t.Fatal(activity.SourceFromRequest(r))
	}
	// X-Grain-Client wins over UA; custom short label
	r.Header.Set("X-Grain-Client", "CustomTool")
	if activity.SourceFromRequest(r) != "customtool" {
		t.Fatal(activity.SourceFromRequest(r))
	}
	// long custom label truncated
	r.Header.Set("X-Grain-Client", strings.Repeat("x", 40))
	got := activity.SourceFromRequest(r)
	if len(got) != 24 {
		t.Fatalf("truncate len %d %q", len(got), got)
	}
	// known sources via header
	for _, src := range []string{"cli", "desktop", "mcp", "sdk", "api"} {
		r.Header.Set("X-Grain-Client", src)
		if activity.SourceFromRequest(r) != src {
			t.Fatalf("header %s", src)
		}
	}

	// UA-based detection (no X-Grain-Client)
	cases := []struct {
		ua, want string
	}{
		{"grain-desktop/1.0", "desktop"},
		{"grain-mcp/0.1", "mcp"},
		{"grain-sdk/2", "sdk"},
		{"grain/0.9 client", "sdk"},
		{"", "api"},
		{"Go-http-client/1.1", "api"},
		{"go-http-client/2.0", "api"},
		{"Mozilla/5.0 curl", "api"},
	}
	for _, tc := range cases {
		req, _ := http.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("User-Agent", tc.ua)
		if got := activity.SourceFromRequest(req); got != tc.want {
			t.Fatalf("ua %q: got %q want %q", tc.ua, got, tc.want)
		}
	}
}

func TestShouldRecord(t *testing.T) {
	t.Parallel()
	if activity.ShouldRecord("GET", "/vms") {
		t.Fatal("list should not record")
	}
	if !activity.ShouldRecord("POST", "/vms") {
		t.Fatal("create should record")
	}
	if !activity.ShouldRecord("GET", "/vms/x/shell") {
		t.Fatal("shell should record")
	}
	if !activity.ShouldRecord("GET", "/vms/x/shell?cols=80") {
		t.Fatal("shell with query should record")
	}
	if activity.ShouldRecord("GET", "/activity") {
		t.Fatal("activity poll should not record")
	}
	if activity.ShouldRecord(http.MethodHead, "/vms") {
		t.Fatal("HEAD should not record")
	}
	if activity.ShouldRecord(http.MethodOptions, "/vms") {
		t.Fatal("OPTIONS should not record")
	}
	for _, p := range []string{"/healthz", "/info", "/metrics", "/openapi.yaml", "/openapi.json", "/activity"} {
		if activity.ShouldRecord("POST", p) {
			t.Fatalf("%s should not record", p)
		}
	}
	if activity.ShouldRecord("POST", "/metrics/scrape") {
		t.Fatal("/metrics prefix should not record")
	}
	if !activity.ShouldRecord("DELETE", "/vms/x") {
		t.Fatal("delete should record")
	}
	if !activity.ShouldRecord("PUT", "/secrets/x") {
		t.Fatal("secret put should record")
	}
}
