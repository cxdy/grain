package client_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
)

// errServer returns configurable status/body for every request.
func errServer(t *testing.T, status int, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
}

func TestClientErrorPathsAllMethods(t *testing.T) {
	t.Parallel()
	// Non-2xx JSON error
	srv := errServer(t, 500, `{"error":"boom"}`)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.Health(ctx); err == nil {
		t.Fatal("health")
	}
	if _, err := c.Info(ctx); err == nil {
		t.Fatal("info")
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("list")
	}
	if _, err := c.Create(ctx, client.CreateRequest{Name: "x"}); err == nil {
		t.Fatal("create")
	}
	if _, err := c.Get(ctx, "x"); err == nil {
		t.Fatal("get")
	}
	if err := c.Delete(ctx, "x"); err == nil {
		t.Fatal("delete")
	}
	if _, err := c.Start(ctx, "x"); err == nil {
		t.Fatal("start")
	}
	if err := c.Stop(ctx, "x"); err == nil {
		t.Fatal("stop")
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
	if err := c.RemoveForward(ctx, "x", 8080); err == nil {
		t.Fatal("rmfwd")
	}
	if _, err := c.Exec(ctx, "x", "true"); err == nil {
		t.Fatal("exec")
	}
	if _, err := c.AgentHealth(ctx, "x"); err == nil {
		t.Fatal("agent health")
	}
	if _, err := c.Stats(ctx, "x"); err == nil {
		t.Fatal("stats")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("list secrets")
	}
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "dg=="}); err == nil {
		t.Fatal("set secret")
	}
	if err := c.DeleteSecret(ctx, "k"); err == nil {
		t.Fatal("del secret")
	}
	if _, err := c.InjectSecret(ctx, "x", "k", ""); err == nil {
		t.Fatal("inject")
	}
	if _, err := c.InjectSecret(ctx, "x", "k", "/run/s"); err == nil {
		t.Fatal("inject path")
	}
	if err := c.PutFile(ctx, "x", "/a", strings.NewReader("hi"), 2, client.CPOpts{}); err == nil {
		t.Fatal("putfile")
	}
	if err := c.GetFile(ctx, "x", "/a", io.Discard); err == nil {
		t.Fatal("getfile")
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
		t.Fatal("remove")
	}
}

func TestClientBadJSONBodies(t *testing.T) {
	t.Parallel()
	srv := errServer(t, 200, `not-json{`)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.Info(ctx); err == nil {
		t.Fatal("info decode")
	}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("list decode")
	}
	if _, err := c.Get(ctx, "x"); err == nil {
		t.Fatal("get decode")
	}
	if _, err := c.Start(ctx, "x"); err == nil {
		t.Fatal("start decode")
	}
	if _, err := c.Restore(ctx, "x"); err == nil {
		t.Fatal("restore decode")
	}
	if _, err := c.AddForward(ctx, "x", 1, 2); err == nil {
		t.Fatal("fwd decode")
	}
	if _, err := c.Exec(ctx, "x", "echo"); err == nil {
		t.Fatal("exec decode")
	}
	if _, err := c.AgentHealth(ctx, "x"); err == nil {
		t.Fatal("agent decode")
	}
	if _, err := c.Stats(ctx, "x"); err == nil {
		t.Fatal("stats decode")
	}
	if _, err := c.ListSecrets(ctx); err == nil {
		t.Fatal("secrets decode")
	}
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "a", DataBase64: "Yg=="}); err == nil {
		t.Fatal("setsecret decode")
	}
	if _, err := c.ReadDir(ctx, "x", "/"); err == nil {
		t.Fatal("readdir decode")
	}
	if _, err := c.Stat(ctx, "x", "/a"); err == nil {
		t.Fatal("stat decode")
	}
}

func TestClientClosedServer(t *testing.T) {
	t.Parallel()
	srv := errServer(t, 200, `{}`)
	u := srv.URL
	srv.Close()
	c, err := client.DialHTTP(u, "tok")
	if err != nil {
		t.Fatal(err)
	}
	c.SetToken("tok2")
	if c.Token() != "tok2" {
		t.Fatal(c.Token())
	}
	if c.Base() != u {
		t.Fatal(c.Base())
	}
	ctx := context.Background()
	if err := c.Health(ctx); err == nil {
		t.Fatal("expected dial error")
	}
	if _, err := c.CreateStream(ctx, client.CreateRequest{Name: "n"}, nil); err == nil {
		t.Fatal("createstream")
	}
	_, _ = c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "true"}, nil)
}

func TestCreateStreamPartialAndInvalid(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") != "1" {
			http.Error(w, "no", 400)
			return
		}
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		// Ready with name only (no nested instance object).
		_, _ = io.WriteString(w, `{"phase":"qemu","message":"boot"}`+"\n")
		_, _ = io.WriteString(w, `{"phase":"ready","name":"only-name"}`+"\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var phases []string
	inst, err := c.CreateStream(context.Background(), client.CreateRequest{Name: "only-name"}, func(ev client.CreateEvent) {
		phases = append(phases, ev.Phase)
	})
	if err != nil {
		t.Fatal(err)
	}
	if inst == nil || inst.Name != "only-name" {
		t.Fatalf("%+v", inst)
	}
	_ = phases
}

func TestExecStreamStdoutStderrExit(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `{"type":"started","pid":9}`+"\n")
		_, _ = io.WriteString(w, `{"type":"stdout","data":"out\n"}`+"\n")
		_, _ = io.WriteString(w, `{"type":"stderr","data":"err\n"}`+"\n")
		_, _ = io.WriteString(w, `{"type":"exit","exit_code":3}`+"\n")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	var frames int
	code, err := c.ExecStream(context.Background(), "vm", client.ExecOpts{
		Cmd: "sh", Args: []string{"-c", "x"},
	}, func(f client.ExecFrame) error {
		frames++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if code != 3 || frames < 3 {
		t.Fatalf("code=%d frames=%d", code, frames)
	}
}

func TestPutGetFileSuccessPaths(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		if r.URL.Query().Get("mode") == "tar" {
			_, _ = w.Write([]byte("tardata"))
			return
		}
		_, _ = w.Write([]byte("payload"))
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	uid, gid := uint32(1), uint32(2)
	if err := c.PutFile(ctx, "vm", "/tmp/a", strings.NewReader("hi"), 2, client.CPOpts{
		Mode: "0644", UID: &uid, GID: &gid,
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "vm", "/tmp/a", &buf); err != nil || buf.String() != "payload" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.PutTar(ctx, "vm", "/tmp", strings.NewReader("t")); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := c.GetTar(ctx, "vm", "/tmp", &buf); err != nil || buf.String() != "tardata" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.Mkdir(ctx, "vm", "/tmp/d", true, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, "vm", "/tmp/d", true); err != nil {
		t.Fatal(err)
	}
}
