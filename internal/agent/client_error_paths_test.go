package agent_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
)

func agentErrSrv(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestAgentClientErrorAndSuccessPaths(t *testing.T) {
	t.Parallel()
	// errors
	es := agentErrSrv(t, 500, "nope")
	t.Cleanup(es.Close)
	ec := &agent.Client{BaseURL: es.URL, HTTP: es.Client()}
	ctx := context.Background()
	if _, err := ec.Health(ctx); err == nil {
		t.Fatal("health")
	}
	if err := ec.HeadHealth(ctx); err == nil {
		t.Fatal("head")
	}
	if _, err := ec.ExecBuffered(ctx, "true"); err == nil {
		t.Fatal("exec")
	}
	if _, err := ec.Stats(ctx); err == nil {
		t.Fatal("stats")
	}
	if err := ec.PutFile(ctx, "/a", strings.NewReader("x"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("put")
	}
	if err := ec.GetFile(ctx, "/a", io.Discard); err == nil {
		t.Fatal("get")
	}
	if err := ec.PutTar(ctx, "/a", strings.NewReader("t")); err == nil {
		t.Fatal("puttar")
	}
	if err := ec.GetTar(ctx, "/a", io.Discard); err == nil {
		t.Fatal("gettar")
	}
	if _, err := ec.ReadDir(ctx, "/"); err == nil {
		t.Fatal("readdir")
	}
	if _, err := ec.Stat(ctx, "/a"); err == nil {
		t.Fatal("stat")
	}
	if err := ec.Mkdir(ctx, "/d", true, "0755"); err == nil {
		t.Fatal("mkdir")
	}
	if err := ec.Remove(ctx, "/a", true); err == nil {
		t.Fatal("rm")
	}
	if _, err := ec.MaterializeSecret(ctx, agent.MaterializeSecretRequest{Name: "k"}); err == nil {
		t.Fatal("mat")
	}
	_, _ = ec.ExecStream(ctx, agent.ExecOpts{Cmd: "true"}, nil)

	// success surface
	mux := http.NewServeMux()
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"hostname":"g","agent_version":"1"}`)
	})
	mux.HandleFunc("HEAD /health", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stdout","data":"hi\n"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stderr","data":"e\n"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
			return
		}
		_, _ = io.WriteString(w, `{"stdout":"hi\n","exit_code":0}`)
	})
	mux.HandleFunc("GET /stats", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"uptime_sec":1.5}`)
	})
	mux.HandleFunc("POST /cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "tar" {
			_, _ = w.Write([]byte("tar"))
			return
		}
		_, _ = w.Write([]byte("bin"))
	})
	mux.HandleFunc("GET /fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `[{"name":"a","type":"file","size":1,"mode":"0644"}]`)
	})
	mux.HandleFunc("GET /fs/stat", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"name":"a","type":"file","size":1,"mode":"0644"}`)
	})
	mux.HandleFunc("POST /fs/mkdir", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("DELETE /fs/remove", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("POST /secrets/materialize", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, `{"path":"/run/s","bytes":1}`)
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &agent.Client{BaseURL: srv.URL, HTTP: srv.Client()}

	if _, err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.HeadHealth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecBuffered(ctx, "echo", "hi"); err != nil {
		t.Fatal(err)
	}
	code, err := c.ExecStream(ctx, agent.ExecOpts{Cmd: "true"}, func(f agent.ExecFrame) error { return nil })
	if err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	if _, err := c.Stats(ctx); err != nil {
		t.Fatal(err)
	}
	uid := uint32(0)
	if err := c.PutFile(ctx, "/a", strings.NewReader("x"), 1, agent.CPOpts{Mode: "0644", UID: &uid}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "/a", &buf); err != nil || buf.String() != "bin" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.PutTar(ctx, "/a", strings.NewReader("t")); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := c.GetTar(ctx, "/a", &buf); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ReadDir(ctx, "/"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stat(ctx, "/a"); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir(ctx, "/d", true, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "/d", true); err != nil {
		t.Fatal(err)
	}
	if _, err := c.MaterializeSecret(ctx, agent.MaterializeSecretRequest{Name: "k", Path: "/run/s"}); err != nil {
		t.Fatal(err)
	}
}

func TestAgentClientBadJSON(t *testing.T) {
	t.Parallel()
	srv := agentErrSrv(t, 200, `not-json`)
	t.Cleanup(srv.Close)
	c := &agent.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	if _, err := c.Health(ctx); err == nil {
		t.Fatal("health")
	}
	if _, err := c.ExecBuffered(ctx, "x"); err == nil {
		t.Fatal("exec")
	}
	if _, err := c.Stats(ctx); err == nil {
		t.Fatal("stats")
	}
	if _, err := c.ReadDir(ctx, "/"); err == nil {
		t.Fatal("rd")
	}
	if _, err := c.Stat(ctx, "/a"); err == nil {
		t.Fatal("stat")
	}
}

