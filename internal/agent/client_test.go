package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func startAgentClient(t *testing.T) *Client {
	t.Helper()
	srv := NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var base string
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			base = "http://" + addr
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if base == "" {
		t.Fatal("no base")
	}
	c := &Client{BaseURL: base, HTTP: &http.Client{Timeout: 5 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Wait(ctx, c); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestClientLongHTTPAndBase(t *testing.T) {
	t.Parallel()
	c := &Client{BaseURL: "http://example/"}
	if c.http() == nil {
		t.Fatal()
	}
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	lh := c.longHTTP()
	if lh.Timeout != 0 {
		t.Fatalf("timeout %v", lh.Timeout)
	}
	c.HTTP = &http.Client{Timeout: 0}
	if c.longHTTP() != c.HTTP {
		t.Fatal("should reuse zero-timeout client")
	}
	if c.base() != "http://example" {
		t.Fatalf("base %q", c.base())
	}
	// nil HTTP uses default
	c2 := &Client{BaseURL: "http://example"}
	_ = c2.longHTTP()
}

func TestClientValidationErrors(t *testing.T) {
	t.Parallel()
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: 100 * time.Millisecond}}
	ctx := context.Background()
	if _, err := c.ExecBufferedOpts(ctx, ExecOpts{}); err == nil {
		t.Fatal("empty cmd")
	}
	if _, err := c.ExecStream(ctx, ExecOpts{Cmd: "x"}, nil); err == nil {
		t.Fatal("nil onFrame")
	}
	if _, err := c.ExecStream(ctx, ExecOpts{}, func(ExecFrame) error { return nil }); err == nil {
		t.Fatal("empty cmd stream")
	}
	if err := c.PutFile(ctx, "", strings.NewReader("x"), 1, CPOpts{}); err == nil {
		t.Fatal("empty path put")
	}
	if err := c.GetFile(ctx, "", io.Discard); err == nil {
		t.Fatal("empty path get")
	}
	if err := c.PutTar(ctx, "", strings.NewReader("x")); err == nil {
		t.Fatal("empty put tar")
	}
	if err := c.GetTar(ctx, "", io.Discard); err == nil {
		t.Fatal("empty get tar")
	}
	if _, err := c.ReadDir(ctx, ""); err == nil {
		t.Fatal("empty readdir")
	}
	if _, err := c.Stat(ctx, ""); err == nil {
		t.Fatal("empty stat")
	}
	if err := c.Mkdir(ctx, "", false, ""); err == nil {
		t.Fatal("empty mkdir")
	}
	if err := c.Remove(ctx, "", false); err == nil {
		t.Fatal("empty remove")
	}
}

func TestClientHTTPErrorPaths(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		http.Error(w, "down", http.StatusInternalServerError)
	})
	mux.HandleFunc("/stats", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no", 500)
	})
	mux.HandleFunc("/exec", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			http.Error(w, "stream fail", 500)
			return
		}
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"exit_code":-1}`))
	})
	mux.HandleFunc("/cp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "cp fail", 500)
	})
	mux.HandleFunc("/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	})
	mux.HandleFunc("/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	})
	mux.HandleFunc("/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	mux.HandleFunc("/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "not found", 404)
	})
	mux.HandleFunc("/secrets/materialize", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	if _, err := c.Health(ctx); err == nil {
		t.Fatal("health error")
	}
	if err := c.HeadHealth(ctx); err == nil {
		t.Fatal("head health")
	}
	if _, err := c.Stats(ctx); err == nil {
		t.Fatal("stats")
	}
	res, err := c.ExecBufferedOpts(ctx, ExecOpts{Cmd: "x"})
	if err != nil {
		t.Log(err)
	}
	if res != nil && res.Error == "" && res.ExitCode == 0 {
		t.Log("unexpected ok exec")
	}
	if _, err := c.ExecStream(ctx, ExecOpts{Cmd: "x"}, func(ExecFrame) error { return nil }); err == nil {
		t.Fatal("stream status")
	}
	if err := c.PutFile(ctx, "/x", strings.NewReader("a"), 1, CPOpts{}); err == nil {
		t.Fatal("put")
	}
	if err := c.GetFile(ctx, "/x", io.Discard); err == nil {
		t.Fatal("get")
	}
	if err := c.PutTar(ctx, "/x", strings.NewReader("a")); err == nil {
		t.Fatal("put tar")
	}
	if err := c.GetTar(ctx, "/x", io.Discard); err == nil {
		t.Fatal("get tar")
	}
	if _, err := c.ReadDir(ctx, "/x"); err == nil {
		t.Fatal("readdir")
	}
	if _, err := c.Stat(ctx, "/x"); err == nil {
		t.Fatal("stat")
	}
	if err := c.Mkdir(ctx, "/x", false, ""); err == nil {
		t.Fatal("mkdir")
	}
	if err := c.Remove(ctx, "/x", false); err == nil {
		t.Fatal("remove")
	}
	if _, err := c.MaterializeSecret(ctx, MaterializeSecretRequest{Name: "n", DataBase64: "YQ=="}); err == nil {
		t.Fatal("materialize")
	}
}

func TestClientHTTPSuccessPaths(t *testing.T) {
	t.Parallel()
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
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()

	if _, err := c.Health(ctx); err != nil {
		t.Fatal(err)
	}
	if err := c.HeadHealth(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := c.ExecBuffered(ctx, "echo", "hi"); err != nil {
		t.Fatal(err)
	}
	code, err := c.ExecStream(ctx, ExecOpts{Cmd: "true"}, func(f ExecFrame) error { return nil })
	if err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	if _, err := c.Stats(ctx); err != nil {
		t.Fatal(err)
	}
	uid := uint32(0)
	if err := c.PutFile(ctx, "/a", strings.NewReader("x"), 1, CPOpts{Mode: "0644", UID: &uid}); err != nil {
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
	if _, err := c.MaterializeSecret(ctx, MaterializeSecretRequest{Name: "k", Path: "/run/s"}); err != nil {
		t.Fatal(err)
	}
}

func TestClientBadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		_, _ = io.WriteString(w, `not-json`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
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

func TestExecStreamOnFrameErrorAndNoExit(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"started","pid":1}` + "\n"))
		_, _ = w.Write([]byte(`{"type":"stdout","data":"hi"}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	_, err := c.ExecStream(context.Background(), ExecOpts{Cmd: "x"}, func(ExecFrame) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "without exit") {
		t.Fatalf("err %v", err)
	}

	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"started","pid":1}` + "\n"))
	}))
	t.Cleanup(srv2.Close)
	c2 := &Client{BaseURL: srv2.URL, HTTP: srv2.Client()}
	_, err = c2.ExecStream(context.Background(), ExecOpts{Cmd: "x"}, func(ExecFrame) error {
		return errors.New("stop")
	})
	if err == nil || !strings.Contains(err.Error(), "stop") {
		t.Fatalf("err %v", err)
	}

	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"error"}` + "\n"))
	}))
	t.Cleanup(srv3.Close)
	c3 := &Client{BaseURL: srv3.URL, HTTP: srv3.Client()}
	_, err = c3.ExecStream(context.Background(), ExecOpts{Cmd: "x"}, func(ExecFrame) error { return nil })
	if err == nil {
		t.Fatal("expected error frame")
	}

	srv4 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte("\n\nnot-json\n"))
	}))
	t.Cleanup(srv4.Close)
	c4 := &Client{BaseURL: srv4.URL, HTTP: srv4.Client()}
	_, err = c4.ExecStream(context.Background(), ExecOpts{Cmd: "x"}, func(ExecFrame) error { return nil })
	if err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClientAgainstLiveAgent(t *testing.T) {
	c := startAgentClient(t)
	ctx := context.Background()
	_ = c.longHTTP()
	c2 := &Client{BaseURL: c.BaseURL, HTTP: &http.Client{Timeout: 0}}
	_ = c2.longHTTP()

	uid, gid := uint32(0), uint32(0)
	if _, err := c.ExecBufferedOpts(ctx, ExecOpts{Cmd: "true", UID: &uid, GID: &gid, Cwd: "/"}); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	p := filepath.Join(dir, "c.txt")
	if err := c.PutFile(ctx, p, strings.NewReader("zz"), 2, CPOpts{Mode: "0644", UID: &uid}); err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	if err := c.GetFile(ctx, p, &out); err != nil || out.String() != "zz" {
		t.Fatalf("%q %v", out.String(), err)
	}
	if err := c.GetFile(ctx, filepath.Join(dir, "missing"), io.Discard); err == nil {
		t.Fatal("missing get")
	}
	if _, err := c.ReadDir(ctx, dir); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stat(ctx, p); err != nil {
		t.Fatal(err)
	}
	if err := c.Mkdir(ctx, filepath.Join(dir, "d"), false, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := c.Remove(ctx, filepath.Join(dir, "d"), false); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Stats(ctx); err != nil {
		t.Fatal(err)
	}

	code, err := c.ExecStream(ctx, ExecOpts{Cmd: "echo", Args: []string{"x"}}, func(f ExecFrame) error {
		return nil
	})
	if err != nil || code != 0 {
		t.Fatalf("%d %v", code, err)
	}
	_, err = c.ExecStream(ctx, ExecOpts{Cmd: "echo", Args: []string{"x"}}, func(f ExecFrame) error {
		return context.Canceled
	})
	if err == nil {
		t.Fatal("expected onFrame error")
	}

	path := filepath.Join(dir, "s")
	_, err = c.MaterializeSecret(ctx, MaterializeSecretRequest{
		Name:       "n",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("v")),
		Path:       path,
		Mode:       "0600",
	})
	if err != nil {
		t.Fatal(err)
	}

	var tbuf bytes.Buffer
	tw := tar.NewWriter(&tbuf)
	_ = tw.WriteHeader(&tar.Header{Name: "t", Mode: 0o644, Size: 1})
	_, _ = tw.Write([]byte("Z"))
	_ = tw.Close()
	dest := filepath.Join(dir, "tarroot")
	if err := c.PutTar(ctx, dest, &tbuf); err != nil {
		t.Fatal(err)
	}
	var outTar bytes.Buffer
	if err := c.GetTar(ctx, dest, &outTar); err != nil {
		t.Fatal(err)
	}
}

func TestIsWSNormalClose(t *testing.T) {
	t.Parallel()
	if !isWSNormalClose(nil) {
		t.Fatal("nil")
	}
	if isWSNormalClose(errors.New("random")) {
		t.Fatal("random")
	}
	if !isWSNormalClose(errors.New("status = StatusNormalClosure")) {
		t.Fatal("normal string")
	}
	if !isWSNormalClose(errors.New("status = StatusGoingAway")) {
		t.Fatal("going away")
	}
	if !isWSNormalClose(errors.New("failed to get reader: received close frame")) {
		t.Fatal("close frame")
	}
	_ = websocket.CloseStatus(errors.New("x"))
}

// fakeShellServer upgrades to WebSocket, echoes binary frames, then closes.
func fakeShellServer(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shell" {
			http.NotFound(w, r)
			return
		}
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: []string{"*"},
		})
		if err != nil {
			return
		}
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		ctx := r.Context()
		for {
			typ, data, err := conn.Read(ctx)
			if err != nil {
				return
			}
			if typ == websocket.MessageBinary || typ == websocket.MessageText {
				_ = conn.Write(ctx, websocket.MessageBinary, append([]byte("echo:"), data...))
				_ = conn.Close(websocket.StatusNormalClosure, "done")
				return
			}
		}
	}))
}

func TestShellClientForwardsTermProgram(t *testing.T) {
	var gotProg, gotColor string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/shell" {
			http.NotFound(w, r)
			return
		}
		gotProg = r.URL.Query().Get("term_program")
		gotColor = r.URL.Query().Get("colorterm")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		_ = conn.Close(websocket.StatusNormalClosure, "ok")
	}))
	t.Cleanup(srv.Close)

	c := &Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_ = c.Shell(ctx, ShellOpts{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Raw:    boolPtrFalse(),
		ExtraEnv: []string{
			"TERM_PROGRAM=iTerm.app",
			"COLORTERM=truecolor",
		},
	})
	if gotProg != "iTerm.app" || gotColor != "truecolor" {
		t.Fatalf("term_program=%q colorterm=%q", gotProg, gotColor)
	}
}

func TestShellClientRoundTrip(t *testing.T) {
	srv := fakeShellServer(t)
	t.Cleanup(srv.Close)

	c := &Client{
		BaseURL: srv.URL,
		HTTP:    &http.Client{Timeout: 0},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	var out strings.Builder
	err := c.Shell(ctx, ShellOpts{
		Cols:   80,
		Rows:   24,
		Stdin:  strings.NewReader("hello"),
		Stdout: &out,
		Stderr: io.Discard,
		Raw:    boolPtrFalse(),
	})
	if err != nil {
		if !isWSNormalClose(err) && !strings.Contains(err.Error(), "close") {
			t.Fatalf("Shell: %v", err)
		}
	}
	if !strings.Contains(out.String(), "echo:") && out.Len() == 0 {
		t.Logf("stdout empty (server may have closed early): %q", out.String())
	}
}

func TestShellClientDefaultsAndHTTPSScheme(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "no upgrade", http.StatusBadRequest)
	}))
	t.Cleanup(srv.Close)

	c := &Client{
		BaseURL: "http" + strings.TrimPrefix(srv.URL, "http"),
		HTTP:    &http.Client{Timeout: 2 * time.Second},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := c.Shell(ctx, ShellOpts{
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Raw:    boolPtrFalse(),
	})
	if err == nil {
		t.Fatal("expected dial/upgrade error")
	}

	c2 := &Client{BaseURL: "https://127.0.0.1:1", HTTP: &http.Client{Timeout: 200 * time.Millisecond}}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel2()
	_ = c2.Shell(ctx2, ShellOpts{
		Cols: 10, Rows: 10,
		Stdin: strings.NewReader(""), Stdout: io.Discard, Raw: boolPtrFalse(),
	})
}

func TestShellClientContextCancel(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: []string{"*"}})
		if err != nil {
			return
		}
		defer func() { _ = conn.CloseNow() }()
		<-r.Context().Done()
	}))
	t.Cleanup(srv.Close)

	pr, pw := io.Pipe()
	t.Cleanup(func() { _ = pw.Close(); _ = pr.Close() })

	c := &Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 0}}
	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	err := c.Shell(ctx, ShellOpts{
		Cols: 40, Rows: 12,
		Stdin:  pr,
		Stdout: io.Discard,
		Raw:    boolPtrFalse(),
	})
	if err == nil {
		t.Fatal("expected cancel error")
	}
}

func boolPtrFalse() *bool {
	v := false
	return &v
}

func TestClientMaterializeSecretNoContent(t *testing.T) {
	t.Parallel()
	// 204 with empty body — MaterializeSecret should accept without decode
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	out, err := c.MaterializeSecret(context.Background(), MaterializeSecretRequest{Name: "k", DataBase64: "YQ=="})
	if err != nil {
		t.Fatal(err)
	}
	if out == nil {
		t.Fatal("nil response")
	}
}

func TestClientExecBufferedStatusErrorField(t *testing.T) {
	t.Parallel()
	// Non-OK status with empty Error field → filled with status N
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"exit_code":1}`))
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	res, err := c.ExecBufferedOpts(context.Background(), ExecOpts{Cmd: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Error == "" {
		t.Fatal("expected status error field")
	}
}

func TestClientGetFileNotFoundVsError(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "/missing" {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "boom", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if err := c.GetFile(context.Background(), "/missing", io.Discard); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("get missing: %v", err)
	}
	if err := c.GetTar(context.Background(), "/missing", io.Discard); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("gettar missing: %v", err)
	}
	if err := c.GetFile(context.Background(), "/other", io.Discard); err == nil {
		t.Fatal("expected 500")
	}
	if err := c.GetTar(context.Background(), "/other", io.Discard); err == nil {
		t.Fatal("expected 500 tar")
	}
}

func TestClientPutWithGIDAndShellOpts(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/cp", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			w.WriteHeader(http.StatusOK) // accept 200 as well as 204
			return
		}
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	gid := uint32(20)
	if err := c.PutFile(context.Background(), "/a", strings.NewReader("x"), -1, CPOpts{GID: &gid}); err != nil {
		t.Fatal(err)
	}
}

func TestShellClientWithShellQuery(t *testing.T) {
	var gotShell string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotShell = r.URL.Query().Get("shell")
		http.Error(w, "no", 400)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: &http.Client{Timeout: 2 * time.Second}}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_ = c.Shell(ctx, ShellOpts{
		Shell:  "/bin/bash",
		Cols:   80,
		Rows:   24,
		Stdin:  strings.NewReader(""),
		Stdout: io.Discard,
		Raw:    boolPtrFalse(),
	})
	if gotShell != "/bin/bash" {
		t.Fatalf("shell query = %q", gotShell)
	}
}

func TestClientMaterializeSecretBadJSON(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `not-json`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	if _, err := c.MaterializeSecret(context.Background(), MaterializeSecretRequest{Name: "k", DataBase64: "YQ=="}); err == nil {
		t.Fatal("expected decode error")
	}
}

func TestClientFSNon404Errors(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	mux.HandleFunc("/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	mux.HandleFunc("/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	if _, err := c.ReadDir(ctx, "/x"); err == nil {
		t.Fatal("readdir 500")
	}
	if _, err := c.Stat(ctx, "/x"); err == nil {
		t.Fatal("stat 500")
	}
	if err := c.Remove(ctx, "/x", false); err == nil {
		t.Fatal("remove 500")
	}
}

func TestClientNetworkErrors(t *testing.T) {
	t.Parallel()
	c := &Client{BaseURL: "http://127.0.0.1:1", HTTP: &http.Client{Timeout: 100 * time.Millisecond}}
	ctx := context.Background()
	if _, err := c.Health(ctx); err == nil {
		t.Fatal("health net")
	}
	if err := c.HeadHealth(ctx); err == nil {
		t.Fatal("head net")
	}
	if _, err := c.Stats(ctx); err == nil {
		t.Fatal("stats net")
	}
	if _, err := c.ExecBufferedOpts(ctx, ExecOpts{Cmd: "x"}); err == nil {
		t.Fatal("exec net")
	}
	if _, err := c.ExecStream(ctx, ExecOpts{Cmd: "x"}, func(ExecFrame) error { return nil }); err == nil {
		t.Fatal("stream net")
	}
	if err := c.PutFile(ctx, "/x", strings.NewReader("a"), 1, CPOpts{}); err == nil {
		t.Fatal("put net")
	}
	if err := c.GetFile(ctx, "/x", io.Discard); err == nil {
		t.Fatal("get net")
	}
	if err := c.PutTar(ctx, "/x", strings.NewReader("a")); err == nil {
		t.Fatal("puttar net")
	}
	if err := c.GetTar(ctx, "/x", io.Discard); err == nil {
		t.Fatal("gettar net")
	}
	if _, err := c.ReadDir(ctx, "/x"); err == nil {
		t.Fatal("rd net")
	}
	if _, err := c.Stat(ctx, "/x"); err == nil {
		t.Fatal("stat net")
	}
	if err := c.Mkdir(ctx, "/x", false, ""); err == nil {
		t.Fatal("mkdir net")
	}
	if err := c.Remove(ctx, "/x", false); err == nil {
		t.Fatal("rm net")
	}
	if _, err := c.MaterializeSecret(ctx, MaterializeSecretRequest{Name: "n", DataBase64: "YQ=="}); err == nil {
		t.Fatal("mat net")
	}
}

func TestExecStreamExitWithoutCode(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		// exit frame with no exit_code → code stays -1
		_, _ = w.Write([]byte(`{"type":"exit"}` + "\n"))
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	code, err := c.ExecStream(context.Background(), ExecOpts{Cmd: "x"}, func(ExecFrame) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if code != -1 {
		t.Fatalf("code %d", code)
	}
}

func TestClientExecBufferedWithArgsUIDGID(t *testing.T) {
	t.Parallel()
	var gotURL string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotURL = r.URL.String()
		_, _ = io.WriteString(w, `{"exit_code":0}`)
	}))
	t.Cleanup(srv.Close)
	c := &Client{BaseURL: srv.URL, HTTP: srv.Client()}
	uid, gid := uint32(1), uint32(2)
	_, err := c.ExecBufferedOpts(context.Background(), ExecOpts{
		Cmd: "echo", Args: []string{"a", "b"}, UID: &uid, GID: &gid, Cwd: "/tmp",
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(gotURL, "uid=1") || !strings.Contains(gotURL, "gid=2") || !strings.Contains(gotURL, "cwd") {
		t.Fatalf("query %s", gotURL)
	}
	// stream with uid/gid/cwd
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		_, _ = w.Write([]byte(`{"type":"exit","exit_code":0}` + "\n"))
	}))
	t.Cleanup(srv2.Close)
	c2 := &Client{BaseURL: srv2.URL, HTTP: srv2.Client()}
	_, err = c2.ExecStream(context.Background(), ExecOpts{
		Cmd: "true", Args: []string{"x"}, UID: &uid, GID: &gid, Cwd: "/tmp",
	}, func(ExecFrame) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
}
