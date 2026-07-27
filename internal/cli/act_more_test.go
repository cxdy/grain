package cli

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/api"
)

func TestShellQuoteMore(t *testing.T) {
	t.Parallel()
	if g := shellQuote("a/b"); g != "a/b" {
		t.Fatalf("%q", g)
	}
	if g := shellQuote("--flag=1"); g != "--flag=1" {
		t.Fatalf("%q", g)
	}
	if g := shellQuote("$x"); g != "'$x'" {
		t.Fatalf("%q", g)
	}
}

func TestWaitActReadyTimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"stdout": "not yet", "exit_code": 1})
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	err := waitActReady(c, "vm", time.Now().Add(-time.Second))
	if err == nil || !strings.Contains(err.Error(), "timeout") {
		t.Fatalf("%v", err)
	}
}

func TestWaitActReadyOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "READY\n", "exit_code": 0,
		})
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := waitActReady(c, "vm", time.Now().Add(10*time.Second)); err != nil {
		t.Fatal(err)
	}
}

func TestCmdActConstruction(t *testing.T) {
	cfg := ""
	cmd := cmdAct(&cfg)
	if cmd.Use == "" {
		t.Fatal("empty use")
	}
	// stripLeadingDashDash used by RunE path via actOpts
	if g := stripLeadingDashDash([]string{"--", "-l", "x"}); len(g) != 2 || g[0] != "-l" {
		t.Fatalf("%v", g)
	}
}

func TestCmdActDaemonDown(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:1"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdAct(&cfg)
	cmd.SetArgs([]string{"--", "-l"})
	// will fail early on health / dial
	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error")
	}
}

func TestShellJoinEmptyAndMixed(t *testing.T) {
	t.Parallel()
	if g := shellJoin(nil); g != "" {
		t.Fatalf("%q", g)
	}
	if g := shellJoin([]string{"a", "b c"}); !strings.Contains(g, "a ") || !strings.Contains(g, "'") {
		t.Fatalf("%q", g)
	}
}
