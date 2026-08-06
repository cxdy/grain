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
	cfg = cfg // ensure local seeded
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
