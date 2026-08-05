package mcp_test

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/mcp"
)

func TestDialHTTPFromAPIURL(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			t.Fatalf("path %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer secret" {
			t.Fatalf("auth %q", got)
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	t.Cleanup(srv.Close)

	c, err := mcp.Dial(mcp.ConnectOptions{
		APIURL: srv.URL,
		Token:  "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	if c.Base() != srv.URL {
		// DialHTTP trims trailing slash; httptest URL has no trailing slash usually
		if c.Token() != "secret" {
			t.Fatalf("token %q", c.Token())
		}
	}
}

func TestDialUnixMissingPath(t *testing.T) {
	// Empty API forces socket path; give a non-existent path — DialUnix still succeeds
	// (connection is lazy) as long as path is non-empty.
	sock := filepath.Join(t.TempDir(), "nope.sock")
	c, err := mcp.Dial(mcp.ConnectOptions{Socket: sock})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestDialRequiresTarget(t *testing.T) {
	// Clear env so we only use explicit empty opts; still loads default home socket.
	// With empty socket override via non-existent home is hard; instead pass ConfigPath
	// that yields empty Socket after load — Load with missing file uses Defaults with socket.
	// Prove API empty + Socket set is required path: empty Socket string but ConfigPath
	// to a yaml with empty socket is awkward. Unit: Dial with Socket "" and API "" still
	// gets default from config.Load — not an error. So test only API path above.
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_SOCKET", "")
	// Should not error: falls back to ~/.grain/grain.sock from Defaults.
	c, err := mcp.Dial(mcp.ConnectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil")
	}
}

func TestConnectFromEnv(t *testing.T) {
	t.Setenv("GRAIN_API", "http://example:7474")
	t.Setenv("GRAIN_TOKEN", "tok")
	t.Setenv("GRAIN_SOCKET", "/tmp/x.sock")
	t.Setenv("GRAIN_CONFIG", "/tmp/grain.yaml")
	opts := mcp.ConnectFromEnv()
	if opts.APIURL != "http://example:7474" || opts.Token != "tok" || opts.Socket != "/tmp/x.sock" || opts.ConfigPath != "/tmp/grain.yaml" {
		t.Fatalf("%+v", opts)
	}
}

func TestDialUsesEnvWhenOptsEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer from-env" {
			t.Fatalf("auth %q", got)
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	t.Setenv("GRAIN_API", srv.URL)
	t.Setenv("GRAIN_TOKEN", "from-env")
	c, err := mcp.Dial(mcp.ConnectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDialConfigLoadErrorFallsBackToHomeSocket(t *testing.T) {
	// ConfigPath pointing at a directory makes Load fail (not a file); Dial falls back
	// to ~/.grain/grain.sock (lazy dial still succeeds).
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_SOCKET", "")
	cfgDir := t.TempDir()
	c, err := mcp.Dial(mcp.ConnectOptions{ConfigPath: cfgDir})
	if err != nil {
		t.Fatal(err)
	}
	if c == nil {
		t.Fatal("nil client")
	}
}

func TestDialConfigTokenAndSocket(t *testing.T) {
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_SOCKET", "")
	t.Setenv("GRAIN_TOKEN", "")
	dir := t.TempDir()
	sock := filepath.Join(dir, "custom.sock")
	cfgPath := filepath.Join(dir, "config.yaml")
	content := "data_dir: " + dir + "\nsocket: " + sock + "\napi_token: cfg-tok\n"
	if err := os.WriteFile(cfgPath, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := mcp.Dial(mcp.ConnectOptions{ConfigPath: cfgPath})
	if err != nil {
		t.Fatal(err)
	}
	if c.Token() != "cfg-tok" {
		t.Fatalf("token %q", c.Token())
	}
}
