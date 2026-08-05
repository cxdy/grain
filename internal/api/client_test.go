package api_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/cxdy/grain/internal/vm"
)

func apiErrServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestAPIClientAllErrorPaths(t *testing.T) {
	t.Parallel()
	srv := apiErrServer(t, 503, `{"error":"down"}`)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	if _, err := c.Create(ctx, api.CreateRequest{Name: "x"}); err == nil {
		t.Fatal("create")
	}
	if _, err := c.CreateStream(ctx, api.CreateRequest{Name: "x"}, nil); err == nil {
		t.Fatal("createstream")
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
	if _, err := c.AddForward(ctx, "x", 0, 80); err == nil {
		t.Fatal("addfwd")
	}
	if err := c.RemoveForward(ctx, "x", 1); err == nil {
		t.Fatal("rmfwd")
	}
	if _, err := c.Exec(ctx, "x", "true"); err == nil {
		t.Fatal("exec")
	}
	if _, err := c.AgentHealth(ctx, "x"); err == nil {
		t.Fatal("agent")
	}
	if _, err := c.Stats(ctx, "x"); err == nil {
		t.Fatal("stats")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("listsec")
	}
	if _, err := c.SetSecret(ctx, secrets.PutRequest{Name: "k", DataBase64: "YQ=="}); err == nil {
		t.Fatal("setsec")
	}
	if err := c.DeleteSecret(ctx, "k"); err == nil {
		t.Fatal("delsec")
	}
	if _, err := c.InjectSecret(ctx, "x", "k", "/p"); err == nil {
		t.Fatal("inject")
	}
	if err := c.PutFile(ctx, "x", "/a", strings.NewReader("z"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("put")
	}
	if err := c.GetFile(ctx, "x", "/a", io.Discard); err == nil {
		t.Fatal("getf")
	}
	if err := c.PutTar(ctx, "x", "/a", strings.NewReader("t")); err == nil {
		t.Fatal("puttar")
	}
	if err := c.GetTar(ctx, "x", "/a", io.Discard); err == nil {
		t.Fatal("gettar")
	}
	if _, err := c.ReadDir(ctx, "x", "/"); err == nil {
		t.Fatal("readdir")
	}
	if _, err := c.Stat(ctx, "x", "/a"); err == nil {
		t.Fatal("stat")
	}
	if err := c.Mkdir(ctx, "x", "/a", true, "0755"); err == nil {
		t.Fatal("mkdir")
	}
	if err := c.Remove(ctx, "x", "/a", true); err == nil {
		t.Fatal("rm")
	}
	_, _ = c.ExecStream(ctx, "x", agent.ExecOpts{Cmd: "true"}, nil)
}

func TestAPIClientBadJSON(t *testing.T) {
	t.Parallel()
	srv := apiErrServer(t, 200, `not-json`)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	if _, err := c.Get(ctx, "x"); err == nil {
		t.Fatal("get")
	}
	if _, err := c.Start(ctx, "x"); err == nil {
		t.Fatal("start")
	}
	if _, err := c.Restore(ctx, "x"); err == nil {
		t.Fatal("restore")
	}
	if _, err := c.AddForward(ctx, "x", 1, 2); err == nil {
		t.Fatal("fwd")
	}
	if _, err := c.Exec(ctx, "x", "e"); err == nil {
		t.Fatal("exec")
	}
	if _, err := c.AgentHealth(ctx, "x"); err == nil {
		t.Fatal("ah")
	}
	if _, err := c.Stats(ctx, "x"); err == nil {
		t.Fatal("st")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("ls")
	}
	if _, err := c.SetSecret(ctx, secrets.PutRequest{Name: "a", DataBase64: "YQ=="}); err == nil {
		t.Fatal("ss")
	}
	if _, err := c.ReadDir(ctx, "x", "/"); err == nil {
		t.Fatal("rd")
	}
	if _, err := c.Stat(ctx, "x", "/a"); err == nil {
		t.Fatal("stat")
	}
}

func TestAPIClientSuccessSurface(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	okJSON := func(w http.ResponseWriter, body string) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, body)
	}
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, `{"phase":"ready","name":"n","instance":{"name":"n","status":"running"}}`+"\n")
			return
		}
		okJSON(w, `{"name":"n","status":"running"}`)
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"name":"n","status":"running"}`)
	})
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"name":"n","status":"running"}`)
	})
	mux.HandleFunc("POST /vms/{name}/shutdown", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		okJSON(w, `{"message":"ok"}`)
	})
	mux.HandleFunc("POST /vms/{name}/pause", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/resume", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/suspend", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"name":"n","status":"running"}`)
	})
	mux.HandleFunc("POST /vms/{name}/forwards", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		okJSON(w, `{"host_port":9,"guest_port":80,"pid":1}`)
	})
	mux.HandleFunc("DELETE /vms/{name}/forwards/{port}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stdout","data":"hi\n"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
			return
		}
		okJSON(w, `{"stdout":"hi\n","exit_code":0}`)
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"hostname":"g","agent_version":"1"}`)
	})
	mux.HandleFunc("POST /vms/{name}/agent/deploy", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"name":"n","binary":"/tmp/agent","health":{"hostname":"g","agent_version":"1"}}`)
	})
	mux.HandleFunc("GET /vms/{name}/stats", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"uptime_sec":1}`)
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `[]`)
	})
	mux.HandleFunc("POST /secrets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		okJSON(w, `{"name":"k","size":1,"mode":"0600"}`)
	})
	mux.HandleFunc("DELETE /secrets/{name}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/secrets/{secret}", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"path":"/run/s"}`)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("b"))
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `[{"name":"a","type":"file","size":1,"mode":"0644"}]`)
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		okJSON(w, `{"name":"a","type":"file","size":1,"mode":"0644"}`)
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	if _, err := c.Create(ctx, api.CreateRequest{Name: "n", Wait: "agent", Timeout: "30s"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Get(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Shutdown(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Pause(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Resume(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Suspend(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Restore(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddForward(ctx, "n", 0, 80); err != nil {
		t.Fatal(err)
	}
	if err := c.RemoveForward(ctx, "n", 9); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(ctx, "n", "true", "a"); err != nil {
		t.Fatal(err)
	}
	code, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true"}, func(f agent.ExecFrame) error { return nil })
	if err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	if _, err := c.AgentHealth(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if res, err := c.DeployAgent(ctx, "n"); err != nil {
		t.Fatal(err)
	} else if res.Name != "n" || res.Binary == "" {
		t.Fatalf("deploy result %+v", res)
	}
	if _, err := c.Stats(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ListSecrets(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.SetSecret(ctx, secrets.PutRequest{Name: "k", DataBase64: "YQ=="}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteSecret(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", "/run/s"); err != nil {
		t.Fatal(err)
	}
	if err := c.PutFile(ctx, "n", "/a", strings.NewReader("z"), 1, agent.CPOpts{}); err != nil {
		t.Fatal(err)
	}
	if err := c.GetFile(ctx, "n", "/a", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if err := c.PutTar(ctx, "n", "/a", strings.NewReader("t")); err != nil {
		t.Fatal(err)
	}
	if err := c.GetTar(ctx, "n", "/a", &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir(ctx, "n", "/"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stat(ctx, "n", "/a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir(ctx, "n", "/d", true, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "n", "/d", true); err != nil {
		t.Fatal(err)
	}
	_ = bytes.Buffer{}
}

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

func TestAPIClientListHealthToken(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.APIToken = "tok-abc"
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	// no token → unauthorized for list
	c := &api.Client{Base: ts.URL, HTTP: ts.Client()}
	if _, err := c.List(context.Background()); err == nil {
		t.Fatal("expected unauthorized")
	}
	// health public
	if err := c.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	// with token
	c.Token = "tok-abc"
	if _, err := c.List(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(context.Background(), "nope"); err == nil {
		t.Fatal("delete missing")
	}
}

func TestAPIClientFSEmptyPathAndExecStreamEdges(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			// blank + error empty + exit
			_, _ = io.WriteString(w, "\n")
			_, _ = io.WriteString(w, `{"type":"error"}`+"\n")
			return
		}
		_, _ = io.WriteString(w, `{"exit_code":0}`)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(404)
		_, _ = io.WriteString(w, `{"error":"not found"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	if err := c.PutFile(ctx, "n", "", strings.NewReader("x"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("empty put path")
	}
	if err := c.GetFile(ctx, "n", "", io.Discard); err == nil {
		t.Fatal("empty get path")
	}
	if err := c.PutTar(ctx, "n", "", strings.NewReader("x")); err == nil {
		t.Fatal("empty puttar")
	}
	if err := c.GetTar(ctx, "n", "", io.Discard); err == nil {
		t.Fatal("empty gettar")
	}
	if _, err := c.ReadDir(ctx, "n", ""); err == nil {
		t.Fatal("empty readdir")
	}
	if _, err := c.Stat(ctx, "n", ""); err == nil {
		t.Fatal("empty stat")
	}
	if err := c.Mkdir(ctx, "n", "", true, ""); err == nil {
		t.Fatal("empty mkdir")
	}
	if err := c.Remove(ctx, "n", "", false); err == nil {
		t.Fatal("empty rm")
	}
	if _, err := c.Exec(ctx, "n", ""); err == nil {
		t.Fatal("empty cmd")
	}
	uid, gid := uint32(1), uint32(2)
	_, _ = c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true", UID: &uid, GID: &gid, Cwd: "/"}, func(f agent.ExecFrame) error {
		return nil
	})
	// error frame with empty error message
	_, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true"}, func(f agent.ExecFrame) error { return nil })
	if err == nil {
		t.Fatal("expected stream error")
	}
}

func TestAPIClientCreateStreamOnEvent(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		enc := json.NewEncoder(w)
		_ = enc.Encode(vm.CreateEvent{Phase: vm.PhaseQEMU, Message: "q"})
		_ = enc.Encode(vm.CreateEvent{
			Phase:    vm.PhaseReady,
			Name:     "s",
			Instance: &vm.Instance{Name: "s", Status: vm.StatusRunning},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	var n int
	inst, err := c.CreateStream(context.Background(), api.CreateRequest{Name: "s"}, func(ev vm.CreateEvent) {
		n++
	})
	if err != nil || inst.Name != "s" || n < 2 {
		t.Fatalf("inst=%+v n=%d err=%v", inst, n, err)
	}
}

func TestAPIClientNilHTTPAndDecodeStatus(t *testing.T) {
	t.Parallel()
	// decodeAPIError without JSON body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(418)
		_, _ = io.WriteString(w, "teapot")
	}))
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL} // nil HTTP → DefaultClient still works for httptest? No — need client that dials srv.
	c.HTTP = srv.Client()
	if _, err := c.Get(context.Background(), "x"); err == nil {
		t.Fatal("expected error")
	}
}

func TestAPIClientPutFileWithUID(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("uid") == "" || r.URL.Query().Get("permissions") == "" {
			w.WriteHeader(400)
			return
		}
		w.WriteHeader(204)
	})
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		// no body inject
		_, _ = io.WriteString(w, `{"path":"/default"}`)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	uid, gid := uint32(10), uint32(20)
	if err := c.PutFile(context.Background(), "n", "/a", strings.NewReader("z"), 1, agent.CPOpts{
		UID: &uid, GID: &gid, Mode: "0644",
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(context.Background(), "n", "k", ""); err != nil {
		t.Fatal(err)
	}
}

// failTransport always returns an error from RoundTrip.
type failTransport struct{ err error }

func (f failTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, f.err
}

func TestClientTransportAndURLErrors(t *testing.T) {
	t.Parallel()
	ctx := context.Background()
	fail := &http.Client{Transport: failTransport{err: errors.New("dial boom")}}

	// Invalid Base makes url.Parse fail for createVMsURL / Exec paths.
	bad := &api.Client{Base: "http://[::1%zz", HTTP: fail}
	if _, err := bad.Create(ctx, api.CreateRequest{Name: "n"}); err == nil {
		t.Fatal("create bad base")
	}
	if _, err := bad.CreateStream(ctx, api.CreateRequest{Name: "n"}, nil); err == nil {
		t.Fatal("createstream bad base")
	}
	if _, err := bad.Exec(ctx, "n", "true"); err == nil {
		t.Fatal("exec bad base")
	}
	if _, err := bad.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true"}, func(agent.ExecFrame) error { return nil }); err == nil {
		t.Fatal("execstream bad base")
	}
	if err := bad.PutFile(ctx, "n", "/a", strings.NewReader("x"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("putfile bad base")
	}
	if err := bad.GetFile(ctx, "n", "/a", io.Discard); err == nil {
		t.Fatal("getfile bad base")
	}
	if err := bad.PutTar(ctx, "n", "/a", strings.NewReader("x")); err == nil {
		t.Fatal("puttar bad base")
	}
	if err := bad.GetTar(ctx, "n", "/a", io.Discard); err == nil {
		t.Fatal("gettar bad base")
	}
	if _, err := bad.ReadDir(ctx, "n", "/"); err == nil {
		t.Fatal("readdir bad base")
	}
	if _, err := bad.Stat(ctx, "n", "/a"); err == nil {
		t.Fatal("stat bad base")
	}
	if err := bad.Remove(ctx, "n", "/a", false); err == nil {
		t.Fatal("remove bad base")
	}

	// Transport failures hit http().Do error branches.
	c := &api.Client{Base: "http://127.0.0.1:1", HTTP: fail}
	if _, err := c.Create(ctx, api.CreateRequest{Name: "n"}); err == nil {
		t.Fatal("create dial")
	}
	if _, err := c.CreateStream(ctx, api.CreateRequest{Name: "n"}, nil); err == nil {
		t.Fatal("createstream dial")
	}
	if _, err := c.Get(ctx, "n"); err == nil {
		t.Fatal("get dial")
	}
	if _, err := c.Start(ctx, "n"); err == nil {
		t.Fatal("start dial")
	}
	if err := c.Shutdown(ctx, "n"); err == nil {
		t.Fatal("shutdown dial")
	}
	if err := c.Pause(ctx, "n"); err == nil {
		t.Fatal("pause dial")
	}
	if err := c.Resume(ctx, "n"); err == nil {
		t.Fatal("resume dial")
	}
	if err := c.Suspend(ctx, "n"); err == nil {
		t.Fatal("suspend dial")
	}
	if _, err := c.Restore(ctx, "n"); err == nil {
		t.Fatal("restore dial")
	}
	if _, err := c.AddForward(ctx, "n", 1, 2); err == nil {
		t.Fatal("addfwd dial")
	}
	if err := c.RemoveForward(ctx, "n", 1); err == nil {
		t.Fatal("rmfwd dial")
	}
	if _, err := c.Exec(ctx, "n", "true"); err == nil {
		t.Fatal("exec dial")
	}
	if _, err := c.AgentHealth(ctx, "n"); err == nil {
		t.Fatal("agent dial")
	}
	if _, err := c.Stats(ctx, "n"); err == nil {
		t.Fatal("stats dial")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("listsec dial")
	}
	if _, err := c.SetSecret(ctx, secrets.PutRequest{Name: "k", DataBase64: "YQ=="}); err == nil {
		t.Fatal("setsec dial")
	}
	if err := c.DeleteSecret(ctx, "k"); err == nil {
		t.Fatal("delsec dial")
	}
	if _, err := c.InjectSecret(ctx, "n", "k", "/p"); err == nil {
		t.Fatal("inject dial")
	}
	if _, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "true"}, func(agent.ExecFrame) error { return nil }); err == nil {
		t.Fatal("execstream dial")
	}
	if err := c.PutFile(ctx, "n", "/a", strings.NewReader("z"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("put dial")
	}
	if err := c.GetFile(ctx, "n", "/a", io.Discard); err == nil {
		t.Fatal("getf dial")
	}
	if err := c.PutTar(ctx, "n", "/a", strings.NewReader("t")); err == nil {
		t.Fatal("puttar dial")
	}
	if err := c.GetTar(ctx, "n", "/a", io.Discard); err == nil {
		t.Fatal("gettar dial")
	}
	if _, err := c.ReadDir(ctx, "n", "/"); err == nil {
		t.Fatal("readdir dial")
	}
	if _, err := c.Stat(ctx, "n", "/a"); err == nil {
		t.Fatal("stat dial")
	}
	if err := c.Mkdir(ctx, "n", "/a", false, ""); err == nil {
		t.Fatal("mkdir dial")
	}
	if err := c.Remove(ctx, "n", "/a", true); err == nil {
		t.Fatal("rm dial")
	}
	if err := c.Health(ctx); err == nil {
		t.Fatal("health dial")
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("list dial")
	}
	if err := c.Delete(ctx, "n"); err == nil {
		t.Fatal("delete dial")
	}
}

func TestClientCreateDecodeAndExecEdges(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			// ready with Error field on error phase, then hang ends
			_, _ = io.WriteString(w, `{"phase":"error","error":"boom-err"}`+"\n")
			return
		}
		// success path returns non-JSON to hit Create decode error
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `not-json`)
	})
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			switch r.URL.Query().Get("cmd") {
			case "badframe":
				_, _ = io.WriteString(w, `{not-json}`+"\n")
			case "onframe":
				_, _ = io.WriteString(w, `{"type":"stdout","data":"x"}`+"\n")
			case "noexit":
				_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			case "exitnil":
				_, _ = io.WriteString(w, `{"type":"exit"}`+"\n")
			default:
				_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
			}
			return
		}
		// non-JSON error body for Exec
		if r.URL.Query().Get("cmd") == "plainerr" {
			w.WriteHeader(502)
			_, _ = io.WriteString(w, "plain failure")
			return
		}
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"stdout":"ok","exit_code":0}`)
	})
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `not-json`)
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(204)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	if _, err := c.Create(ctx, api.CreateRequest{Name: "n"}); err == nil {
		t.Fatal("create decode")
	}
	if _, err := c.CreateStream(ctx, api.CreateRequest{Name: "n"}, nil); err == nil || !strings.Contains(err.Error(), "boom-err") {
		t.Fatalf("stream err field: %v", err)
	}
	if _, err := c.Exec(ctx, "n", "plainerr"); err == nil || !strings.Contains(err.Error(), "502") {
		t.Fatalf("exec plain err: %v", err)
	}
	if _, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "badframe"}, func(agent.ExecFrame) error { return nil }); err == nil {
		t.Fatal("bad frame")
	}
	if _, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "onframe"}, func(agent.ExecFrame) error {
		return errors.New("stop")
	}); err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("onframe: %v", err)
	}
	if _, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "noexit"}, func(agent.ExecFrame) error { return nil }); err == nil || !strings.Contains(err.Error(), "without exit") {
		t.Fatalf("noexit: %v", err)
	}
	code, err := c.ExecStream(ctx, "n", agent.ExecOpts{Cmd: "exitnil"}, func(agent.ExecFrame) error { return nil })
	if err != nil || code != -1 {
		t.Fatalf("exitnil code=%d err=%v", code, err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", "/p"); err == nil {
		t.Fatal("inject decode")
	}
	// non-recursive remove success
	if err := c.Remove(ctx, "n", "/a", false); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir(ctx, "n", "/a", false, "0700"); err != nil {
		t.Fatal(err)
	}
}

func TestClientCreateStreamErrorFieldAndReadyInstance(t *testing.T) {
	t.Parallel()
	// Create with wait/timeout query + CreateStream ready with full instance already covered;
	// here cover PhaseError with Error field taking precedence over Message.
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_ = json.NewEncoder(w).Encode(vm.CreateEvent{
			Phase:   vm.PhaseError,
			Error:   "from-error",
			Message: "from-message",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	_, err := c.CreateStream(context.Background(), api.CreateRequest{Name: "e", Wait: "auto", Timeout: "5s"}, func(vm.CreateEvent) {})
	if err == nil || !strings.Contains(err.Error(), "from-error") {
		t.Fatalf("err %v", err)
	}
}

func TestClientCreateOKAndInjectNoPath(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("wait") != "ssh" || r.URL.Query().Get("timeout") != "10s" {
			w.WriteHeader(400)
			_, _ = io.WriteString(w, `{"error":"query"}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"name":"ok","status":"running"}`)
	})
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "" {
			w.WriteHeader(400)
			return
		}
		_, _ = io.WriteString(w, `{"path":"/default"}`)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		// size < 0 → ContentLength unset still succeeds
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	inst, err := c.Create(ctx, api.CreateRequest{Name: "ok", Wait: "ssh", Timeout: "10s"})
	if err != nil || inst.Name != "ok" {
		t.Fatalf("%+v %v", inst, err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.PutFile(ctx, "n", "/a", strings.NewReader("z"), -1, agent.CPOpts{}); err != nil {
		t.Fatal(err)
	}
}
