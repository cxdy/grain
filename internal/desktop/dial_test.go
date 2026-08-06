package desktop

import (
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
)

func TestDialConnectionLocalUnix(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	dir, err := os.MkdirTemp("", "gd-dial-*")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	sock := filepath.Join(dir, "g.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close(); _ = ln.Close() })

	cfg := Config{Socket: sock}
	c, err := DialConnection(Connection{Name: "local", Socket: sock}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestDialConnectionLocalTCPFallback(t *testing.T) {
	// No unix socket; daemon on TCP only (matches api: 0.0.0.0:7474 labs).
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "auth", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*client.Instance{{Name: "work", Status: client.StatusRunning}})
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	cfg := Config{
		Socket:   filepath.Join(t.TempDir(), "missing.sock"),
		API:      "0.0.0.0:" + port,
		APIToken: "secret",
	}
	if SocketOK(cfg.Socket) {
		t.Fatal("socket should be missing")
	}
	c, err := DialConnection(Connection{Name: "local"}, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	list, err := c.List(t.Context())
	if err != nil || len(list) != 1 || list[0].Name != "work" {
		t.Fatalf("list: %+v %v", list, err)
	}
}

func TestDialConnectionRemote(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer abc" {
			http.Error(w, "no", http.StatusUnauthorized)
			return
		}
		w.WriteHeader(200)
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })

	api := "http://" + ln.Addr().String()
	c, err := DialConnection(Connection{Name: "lab", API: api, Token: "abc"}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
}

func TestLocalLoopbackAPIURL(t *testing.T) {
	t.Parallel()
	if got := LocalLoopbackAPIURL("0.0.0.0:7474"); got != "http://127.0.0.1:7474" {
		t.Fatalf("got %q", got)
	}
	if got := LocalLoopbackAPIURL("127.0.0.1:9"); got != "http://127.0.0.1:9" {
		t.Fatalf("got %q", got)
	}
	if LocalLoopbackAPIURL("") != "" || LocalLoopbackAPIURL("bad") != "" {
		t.Fatal("empty cases")
	}
}

func TestSocketOK(t *testing.T) {
	if SocketOK("") || SocketOK("/no/such/sock") {
		t.Fatal("expected false")
	}
}

func TestEffectiveToken(t *testing.T) {
	t.Setenv("GRAIN_TOKEN", "from-env")
	if EffectiveToken(Connection{}, Config{APIToken: "cfg"}) != "from-env" {
		t.Fatal("env wins")
	}
	t.Setenv("GRAIN_TOKEN", "")
	if EffectiveToken(Connection{Token: "c"}, Config{APIToken: "cfg"}) != "c" {
		t.Fatal("conn token")
	}
	if EffectiveToken(Connection{}, Config{APIToken: "cfg"}) != "cfg" {
		t.Fatal("cfg token")
	}
}

func TestResolveDialTargetRemoteEmpty(t *testing.T) {
	t.Parallel()
	// empty API is treated as local
	_, err := ResolveDialTarget(Connection{Name: "local"}, Config{})
	if err == nil {
		t.Fatal("want error for undialable local")
	}
}

func TestResolveDialTargetRemote(t *testing.T) {
	t.Parallel()
	dt, err := ResolveDialTarget(Connection{Name: "lab", API: "http://10.0.0.1:7474", Token: "x"}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	if dt.UseUnix || dt.BaseURL != "http://10.0.0.1:7474" || dt.Token != "x" {
		t.Fatalf("%+v", dt)
	}
}

func TestDialConnectionRemoteEmptyAPI(t *testing.T) {
	t.Parallel()
	// Connection with whitespace-only API is local; no sock/api → error
	_, err := DialConnection(Connection{Name: "x", API: "   "}, Config{})
	if err == nil {
		t.Fatal("want error")
	}
}
