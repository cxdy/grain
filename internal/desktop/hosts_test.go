package desktop

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
)

func TestTestConnectionOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
			_, _ = w.Write([]byte(`{"ok":true}`))
		case "/info":
			_ = json.NewEncoder(w).Encode(map[string]string{"name": "grain", "version": "0.7.0"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	p, err := TestConnection(ctx, srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if !p.Reachable || p.Version != "v0.7.0" {
		t.Fatalf("%+v", p)
	}
}

func TestTestConnectionFail(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, err := TestConnection(ctx, "http://127.0.0.1:1", "")
	if err == nil {
		t.Fatal("want error")
	}
}

func TestProbeConnectionsLocalDial(t *testing.T) {
	// Use a dial that always fails for non-empty API and succeeds for local.
	cfg := Defaults()
	cfg.Connections = []Connection{{Name: "lab", API: "http://127.0.0.1:1"}}
	cfg.Connections = EnsureLocalConnection(cfg.Connections, cfg.Socket, cfg.DataDir)
	dial := func(conn Connection, c Config) (*client.Client, error) {
		if conn.IsLocal() {
			return nil, context.DeadlineExceeded // local may be down — still a probe result
		}
		return nil, context.DeadlineExceeded
	}
	probes := ProbeConnections(context.Background(), cfg, dial)
	if len(probes) < 1 {
		t.Fatal("want probes")
	}
	for _, p := range probes {
		if p.Reachable {
			t.Fatalf("expected unreachable: %+v", p)
		}
	}
}

func TestProbeConnectionsReachable(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "9.0.0"}) // no v prefix
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	cfg := Defaults()
	cfg.Connections = []Connection{{Name: "lab", API: srv.URL}}
	// skip local by only using lab — ActiveConnections prepends local; dial local fails health
	probes := ProbeConnections(context.Background(), cfg, nil)
	if len(probes) < 1 {
		t.Fatal("want probes")
	}
	found := false
	for _, p := range probes {
		if p.Name == "lab" {
			found = true
			if !p.Reachable || p.Version != "v9.0.0" {
				t.Fatalf("%+v", p)
			}
		}
	}
	if !found {
		t.Fatalf("lab missing: %+v", probes)
	}
}

func TestProbeConnectionsHealthFailAfterDial(t *testing.T) {
	// dial succeeds but health fails
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 503)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfg := Defaults()
	cfg.Connections = EnsureLocalConnection([]Connection{{Name: "lab", API: srv.URL}}, cfg.Socket, cfg.DataDir)
	probes := ProbeConnections(context.Background(), cfg, DialConnection)
	for _, p := range probes {
		if p.Name == "lab" && p.Reachable {
			t.Fatalf("want unreachable: %+v", p)
		}
	}
}

func TestTestConnectionEmptyAndToken(t *testing.T) {
	if _, err := TestConnection(context.Background(), "", ""); err == nil {
		t.Fatal("want api required")
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret" {
			http.Error(w, "auth", 401)
			return
		}
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"version": "v1"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p, err := TestConnection(context.Background(), srv.URL, "secret")
	if err != nil || !p.Reachable || p.Version != "v1" {
		t.Fatalf("%+v %v", p, err)
	}
	// health fail returns error
	if _, err := TestConnection(context.Background(), srv.URL, "wrong"); err == nil {
		t.Fatal("want auth error")
	}
}
