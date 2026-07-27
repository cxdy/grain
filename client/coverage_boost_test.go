package client_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
)

func TestClientFullSuccessSurface(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	ok := func(w http.ResponseWriter, v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"status": "ok"})
	})
	mux.HandleFunc("GET /info", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"name": "grain", "version": "t"})
	})
	mux.HandleFunc("GET /vms", func(w http.ResponseWriter, r *http.Request) {
		ok(w, []*client.Instance{{Name: "a", Status: client.StatusRunning}})
	})
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("stream") == "1" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			enc := json.NewEncoder(w)
			_ = enc.Encode(client.CreateEvent{Phase: client.PhaseQEMU})
			_ = enc.Encode(client.CreateEvent{
				Phase:    client.PhaseReady,
				Name:     "n",
				Instance: &client.Instance{Name: "n", Status: client.StatusRunning},
			})
			return
		}
		w.WriteHeader(http.StatusCreated)
		ok(w, &client.Instance{Name: "n", Status: client.StatusRunning})
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("DELETE /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"message": "deleted"})
	})
	mux.HandleFunc("POST /vms/{name}/start", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/shutdown", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"message": "ok"})
	})
	mux.HandleFunc("POST /vms/{name}/pause", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/resume", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/suspend", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/restore", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Instance{Name: r.PathValue("name"), Status: client.StatusRunning})
	})
	mux.HandleFunc("POST /vms/{name}/forwards", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		ok(w, &client.LiveForward{HostPort: 9, GuestPort: 80, PID: 1})
	})
	mux.HandleFunc("DELETE /vms/{name}/forwards/{port}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = io.WriteString(w, "\n")
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stdout","data":"hi"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"stderr","data":"e"}`+"\n")
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
			return
		}
		ok(w, &client.ExecResult{Stdout: "hi", ExitCode: 0})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Health{Hostname: "g", AgentVersion: "1"})
	})
	mux.HandleFunc("GET /vms/{name}/stats", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.Stats{MemTotal: 1, UptimeSec: 1})
	})
	mux.HandleFunc("GET /secrets", func(w http.ResponseWriter, r *http.Request) {
		ok(w, []client.SecretMeta{{Name: "k"}})
	})
	mux.HandleFunc("POST /secrets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(201)
		ok(w, &client.SecretMeta{Name: "k"})
	})
	mux.HandleFunc("DELETE /secrets/{name}", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	mux.HandleFunc("POST /vms/{name}/secrets/{s}", func(w http.ResponseWriter, r *http.Request) {
		ok(w, map[string]string{"path": "/p"})
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "tar" {
			_, _ = w.Write([]byte("tar"))
			return
		}
		_, _ = w.Write([]byte("bin"))
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		ok(w, []client.FSInfo{{Name: "a", Type: "file", Size: 1, Mode: "0644"}})
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		ok(w, &client.FSInfo{Name: "a", Type: "file", Size: 1, Mode: "0644"})
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(204) })

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "tok")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	if err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Info(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Create(ctx, client.CreateRequest{Name: "n", Wait: "ssh", Timeout: "30s"}); err != nil {
		t.Fatal(err)
	}
	var n int
	if _, err := c.CreateStream(ctx, client.CreateRequest{Name: "n"}, func(ev client.CreateEvent) { n++ }); err != nil || n < 1 {
		t.Fatalf("stream n=%d err=%v", n, err)
	}
	if _, err := c.Get(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Delete(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Start(ctx, "n"); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(ctx, "n"); err != nil {
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
	uid, gid := uint32(1), uint32(2)
	code, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "true", UID: &uid, GID: &gid, Cwd: "/tmp"}, func(f client.ExecFrame) error {
		return nil
	})
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
	if _, err := c.SetSecret(ctx, client.SecretPut{Name: "k", DataBase64: "YQ=="}); err != nil {
		t.Fatal(err)
	}
	if err := c.DeleteSecret(ctx, "k"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", "/p"); err != nil {
		t.Fatal(err)
	}
	if _, err := c.InjectSecret(ctx, "n", "k", ""); err != nil {
		t.Fatal(err)
	}
	if err := c.PutFile(ctx, "n", "/a", strings.NewReader("z"), 1, client.CPOpts{UID: &uid, GID: &gid, Mode: "0644"}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, "n", "/a", &buf); err != nil || buf.String() != "bin" {
		t.Fatalf("%q %v", buf.String(), err)
	}
	if err := c.PutTar(ctx, "n", "/a", strings.NewReader("t")); err != nil {
		t.Fatal(err)
	}
	buf.Reset()
	if err := c.GetTar(ctx, "n", "/a", &buf); err != nil {
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
	if err := c.Remove(ctx, "n", "/d", false); err != nil {
		t.Fatal(err)
	}
	if c.Base() == "" || c.Token() != "tok" {
		t.Fatalf("base/token %q %q", c.Base(), c.Token())
	}
}

func TestClientDecodeAPIErrorNoJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = io.WriteString(w, "plain")
	}))
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.List(context.Background()); err == nil || !strings.Contains(err.Error(), "500") {
		t.Fatalf("%v", err)
	}
}

func TestClientExecNonJSONErrorBody(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(502)
		_, _ = io.WriteString(w, "bad gateway")
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Exec(context.Background(), "n", "true"); err == nil {
		t.Fatal("expected error")
	}
}

func TestClientExecStreamNoExitAndBadFrame(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms/{name}/exec", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		switch r.URL.Query().Get("cmd") {
		case "badjson":
			_, _ = io.WriteString(w, "{not-json}\n")
		case "noexit":
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
		case "emptyerr":
			_, _ = io.WriteString(w, `{"type":"error"}`+"\n")
		case "onframe":
			_, _ = io.WriteString(w, `{"type":"started","pid":1}`+"\n")
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
		default:
			_, _ = io.WriteString(w, `{"type":"exit","exit_code":0}`+"\n")
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "badjson"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("badjson")
	}
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "noexit"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("noexit")
	}
	if _, err := c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "emptyerr"}, func(client.ExecFrame) error { return nil }); err == nil {
		t.Fatal("emptyerr")
	}
	_, err = c.ExecStream(ctx, "n", client.ExecOpts{Cmd: "onframe"}, func(client.ExecFrame) error {
		return io.EOF
	})
	if err == nil {
		t.Fatal("onframe")
	}
}

func TestClientCreateStreamBlankLines(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /vms", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = io.WriteString(w, "\n\n")
		_ = json.NewEncoder(w).Encode(client.CreateEvent{
			Phase: client.PhaseReady,
			Name:  "solo",
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	inst, err := c.CreateStream(context.Background(), client.CreateRequest{}, nil)
	if err != nil || inst.Name != "solo" {
		t.Fatalf("%+v %v", inst, err)
	}
}

func TestDialHTTPTrimSlashAndEmpty(t *testing.T) {
	t.Parallel()
	if _, err := client.DialHTTP("", ""); err == nil {
		t.Fatal("empty base")
	}
	if _, err := client.DialUnix(""); err == nil {
		t.Fatal("empty sock")
	}
	c, err := client.DialHTTP("http://example.com/", "t")
	if err != nil {
		t.Fatal(err)
	}
	if c.Base() != "http://example.com" {
		t.Fatalf("%q", c.Base())
	}
	if c.Token() != "t" {
		t.Fatalf("%q", c.Token())
	}
}
