package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/cxdy/grain/internal/vm"
)

func TestCreateVMsURLAndClientErrors(t *testing.T) {
	// Fake daemon that returns errors / stream events
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			// blank line + bad json + error phase without message + ready without instance
			_, _ = w.Write([]byte("\n"))
			_, _ = w.Write([]byte("{not-json}\n"))
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "create failed"})
	})
	mux.HandleFunc("GET /vms/x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte("not-json"))
	})
	mux.HandleFunc("POST /vms/x/start", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot start"})
	})
	mux.HandleFunc("POST /vms/x/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "cannot stop"})
	})
	mux.HandleFunc("POST /vms/x/pause", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "pause fail"})
	})
	mux.HandleFunc("POST /vms/x/resume", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "resume fail"})
	})
	mux.HandleFunc("POST /vms/x/suspend", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "suspend fail"})
	})
	mux.HandleFunc("POST /vms/x/restore", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "restore fail"})
	})
	mux.HandleFunc("DELETE /vms/x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		// no JSON body
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	if _, err := c.Create(ctx, api.CreateRequest{Name: "n"}); err == nil {
		t.Fatal("create error")
	}
	if _, err := c.CreateStream(ctx, api.CreateRequest{Name: "n", Wait: "ssh", Timeout: "30s"}, nil); err == nil {
		t.Fatal("stream decode error")
	}
	if _, err := c.Get(ctx, "x"); err == nil {
		t.Fatal("get")
	}
	if _, err := c.Start(ctx, "x"); err == nil {
		t.Fatal("start")
	}
	if err := c.Shutdown(ctx, "x"); err == nil {
		t.Fatal("shutdown")
	}
	if err := c.Pause(ctx, "x"); err == nil {
		t.Fatal("pause")
	}
	if err := c.Resume(ctx, "x"); err == nil {
		t.Fatal("resume")
	}
	if err := c.Suspend(ctx, "x"); err == nil {
		t.Fatal("suspend")
	}
	if _, err := c.Restore(ctx, "x"); err == nil {
		t.Fatal("restore")
	}
	if err := c.Delete(ctx, "x"); err == nil {
		t.Fatal("delete")
	}
	if err := c.Health(ctx); err == nil {
		t.Fatal("unhealthy")
	}
}

func TestCreateStreamReadyAndErrorPhases(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		evs := []vm.CreateEvent{
			{Phase: vm.PhaseQEMU, Message: "booting"},
			{Phase: vm.PhaseError, Message: "only message"}, // error via Message
		}
		enc := json.NewEncoder(w)
		for _, ev := range evs {
			_ = enc.Encode(ev)
		}
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	_, err := c.CreateStream(context.Background(), api.CreateRequest{Name: "e"}, nil)
	if err == nil || !strings.Contains(err.Error(), "only message") {
		t.Fatalf("err %v", err)
	}

	// ready with name only
	mux2 := http.NewServeMux()
	mux2.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(vm.CreateEvent{
			Phase:   vm.PhaseReady,
			Name:    "ready-only",
			SSHPort: 22,
		})
	})
	ts2 := httptest.NewServer(mux2)
	t.Cleanup(ts2.Close)
	c2 := &api.Client{Base: ts2.URL, HTTP: ts2.Client()}
	inst, err := c2.CreateStream(context.Background(), api.CreateRequest{Name: "r"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if inst.Name != "ready-only" || inst.SSHPort != 22 {
		t.Fatalf("%+v", inst)
	}

	// stream ends without ready
	mux3 := http.NewServeMux()
	mux3.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(vm.CreateEvent{Phase: vm.PhaseQEMU})
	})
	ts3 := httptest.NewServer(mux3)
	t.Cleanup(ts3.Close)
	c3 := &api.Client{Base: ts3.URL, HTTP: ts3.Client()}
	_, err = c3.CreateStream(context.Background(), api.CreateRequest{Name: "r"}, nil)
	if err == nil || !strings.Contains(err.Error(), "without ready") {
		t.Fatalf("err %v", err)
	}

	// error with empty message → create failed
	mux4 := http.NewServeMux()
	mux4.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(vm.CreateEvent{Phase: vm.PhaseError})
	})
	ts4 := httptest.NewServer(mux4)
	t.Cleanup(ts4.Close)
	c4 := &api.Client{Base: ts4.URL, HTTP: ts4.Client()}
	_, err = c4.CreateStream(context.Background(), api.CreateRequest{Name: "r"}, nil)
	if err == nil || !strings.Contains(err.Error(), "create failed") {
		t.Fatalf("err %v", err)
	}
}

func TestClientForwardAndSecretsErrors(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/v/forwards", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "fwd fail"})
	})
	mux.HandleFunc("DELETE /vms/v/forwards/22", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "rm fail"})
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "no secrets"})
	})
	mux.HandleFunc("POST /secrets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "bad secret"})
	})
	mux.HandleFunc("DELETE /secrets/x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "missing"})
	})
	mux.HandleFunc("POST /vms/v/secrets/x", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "inject fail"})
	})
	mux.HandleFunc("GET /vms/v/agent/health", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(503)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "down"})
	})
	mux.HandleFunc("GET /vms/v/stats", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "stats"})
	})
	mux.HandleFunc("POST /vms/v/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.WriteHeader(502)
			_ = json.NewEncoder(w).Encode(map[string]string{"error": "stream fail"})
			return
		}
		w.WriteHeader(502)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "exec fail"})
	})

	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	ctx := context.Background()

	if _, err := c.AddForward(ctx, "v", 0, 80); err == nil {
		t.Fatal("add forward")
	}
	if err := c.RemoveForward(ctx, "v", 22); err == nil {
		t.Fatal("rm forward")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("list secrets")
	}
	if _, err := c.SetSecret(ctx, secrets.PutRequest{Name: "x", DataBase64: "YQ=="}); err == nil {
		t.Fatal("set secret")
	}
	if err := c.DeleteSecret(ctx, "x"); err == nil {
		t.Fatal("del secret")
	}
	if _, err := c.InjectSecret(ctx, "v", "x", "/tmp/s"); err == nil {
		t.Fatal("inject")
	}
	if _, err := c.AgentHealth(ctx, "v"); err == nil {
		t.Fatal("agent health")
	}
	if _, err := c.Stats(ctx, "v"); err == nil {
		t.Fatal("stats")
	}
	if _, err := c.Exec(ctx, "v", "true"); err == nil {
		t.Fatal("exec")
	}
	if _, err := c.ExecStream(ctx, "v", agent.ExecOpts{Cmd: "true"}, func(agent.ExecFrame) error { return nil }); err == nil {
		t.Fatal("exec stream")
	}
}
