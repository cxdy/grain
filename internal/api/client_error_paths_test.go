package api_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/secrets"
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
