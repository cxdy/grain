package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestParseOctalMode(t *testing.T) {
	t.Parallel()
	m, err := parseOctalMode("0644")
	if err != nil || m != 0o644 {
		t.Fatalf("%v %v", m, err)
	}
	m, err = parseOctalMode("0o755")
	if err != nil || m != 0o755 {
		t.Fatalf("%v %v", m, err)
	}
	m, err = parseOctalMode("0O600")
	if err != nil || m != 0o600 {
		t.Fatalf("%v %v", m, err)
	}
	if _, err := parseOctalMode("xyz"); err == nil {
		t.Fatal("expected error")
	}
}

func TestDecodeBase64(t *testing.T) {
	t.Parallel()
	std := base64.StdEncoding.EncodeToString([]byte("hello"))
	b, err := decodeBase64(std)
	if err != nil || string(b) != "hello" {
		t.Fatalf("%q %v", b, err)
	}
	b, err = decodeBase64("\n" + std + "\r\n")
	if err != nil || string(b) != "hello" {
		t.Fatalf("ws: %q %v", b, err)
	}
	raw := base64.RawStdEncoding.EncodeToString([]byte("raw"))
	b, err = decodeBase64(raw)
	if err != nil || string(b) != "raw" {
		t.Fatalf("raw: %q %v", b, err)
	}
}

func TestSafeTarPath(t *testing.T) {
	t.Parallel()
	dest := t.TempDir()
	p, err := safeTarPath(dest, "ok.txt")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, dest) {
		t.Fatalf("%s not under %s", p, dest)
	}
	if _, err := safeTarPath(dest, "../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := safeTarPath(dest, "a/../../escape"); err == nil {
		t.Fatal("expected nested traversal error")
	}
	p, err = safeTarPath(dest, ".")
	if err != nil || p != dest {
		t.Fatalf("dot: %s %v", p, err)
	}
	p, err = safeTarPath(dest, "/etc/passwd")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(p, dest) {
		t.Fatalf("escaped: %s", p)
	}
}

func TestUserdataRanEnv(t *testing.T) {
	t.Setenv("GRAIN_USERDATA_RAN", "1")
	if !userdataRan() {
		t.Fatal("expected env override true")
	}
	t.Setenv("GRAIN_USERDATA_RAN", "true")
	if !userdataRan() {
		t.Fatal("expected true env")
	}
	t.Setenv("GRAIN_USERDATA_RAN", "")
	// without marker file, false (unless real path exists on system)
	_ = userdataRan()
}

func TestFSInfoFromTypes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "f")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(f)
	if err != nil {
		t.Fatal(err)
	}
	info := fsInfoFrom("f", fi)
	if info.Type != "file" || info.Name != "f" {
		t.Fatalf("%+v", info)
	}
	di, err := os.Lstat(dir)
	if err != nil {
		t.Fatal(err)
	}
	dinfo := fsInfoFrom(filepath.Base(dir), di)
	if dinfo.Type != "directory" {
		t.Fatalf("%+v", dinfo)
	}
	link := filepath.Join(dir, "l")
	if err := os.Symlink("f", link); err != nil {
		t.Fatal(err)
	}
	li, err := os.Lstat(link)
	if err != nil {
		t.Fatal(err)
	}
	linfo := fsInfoFrom("l", li)
	if linfo.Type != "symlink" {
		t.Fatalf("%+v", linfo)
	}
}

func TestPutTarSymlinkAndDir(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "sub/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	content := "hi"
	if err := tw.WriteHeader(&tar.Header{Name: "sub/a.txt", Mode: 0, Size: int64(len(content)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, content); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "sub/link", Typeflag: tar.TypeSymlink, Linkname: "a.txt", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "sub/dev", Typeflag: tar.TypeChar, Mode: 0o600}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out")
	if err := putTar(dest, bytes.NewReader(buf.Bytes()), nil, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(dest, "sub", "a.txt"))
	if err != nil || string(got) != "hi" {
		t.Fatalf("%q %v", got, err)
	}
	target, err := os.Readlink(filepath.Join(dest, "sub", "link"))
	if err != nil || target != "a.txt" {
		t.Fatalf("link %q %v", target, err)
	}
}

func TestWriteTarFileAndDir(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "one.txt")
	if err := os.WriteFile(f, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "lnk")
	if err := os.Symlink("one.txt", link); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeTar(&buf, dir); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&buf)
	var names []string
	for {
		hdr, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		names = append(names, hdr.Name)
	}
	if len(names) < 2 {
		t.Fatalf("names %v", names)
	}
	var one bytes.Buffer
	if err := writeTar(&one, f); err != nil {
		t.Fatal(err)
	}
}

func TestCopyFileReplace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFileReplace(src, dst, 0o640); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "payload" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestApplyOwnershipNonRoot(t *testing.T) {
	t.Parallel()
	uid := uint32(1)
	if err := applyOwnership(t.TempDir(), &uid, nil); err != nil {
		t.Fatal(err)
	}
	if err := applyOwnership(t.TempDir(), nil, nil); err != nil {
		t.Fatal(err)
	}
}

func TestApplyCredentialNonRoot(t *testing.T) {
	t.Parallel()
	uid := uint32(1)
	cmd := exec.Command("true")
	applyCredential(cmd, &uid, &uid)
	// not root → SysProcAttr should remain nil
	if os.Geteuid() != 0 && cmd.SysProcAttr != nil {
		t.Fatal("expected no credential when not root")
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
	// Touch CloseStatus the same way isWSNormalClose does.
	_ = websocket.CloseStatus(errors.New("x"))
}

func TestNewServerDefaultsAndShutdown(t *testing.T) {
	s := NewServer("", nil)
	if s.Addr != DefaultListen {
		t.Fatalf("addr %q", s.Addr)
	}
	if s.Log == nil {
		t.Fatal("log")
	}
	if err := s.Shutdown(context.Background()); err != nil {
		t.Fatal(err)
	}
	if s.AddrString() != DefaultListen {
		t.Fatalf("AddrString %q", s.AddrString())
	}
}

func TestHandleExecInvalidUIDGID(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec?cmd=echo&uid=nope", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("uid buffered: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/exec?cmd=echo&gid=nope", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("gid buffered: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/exec?cmd=echo&buffered=false&uid=xx", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "error") {
		t.Fatalf("uid stream: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/exec?cmd=echo&buffered=false&gid=yy", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK || !strings.Contains(rr.Body.String(), "error") {
		t.Fatalf("gid stream: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/exec", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing cmd: %d", rr.Code)
	}
}

func TestHandleCPValidation(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	dir := t.TempDir()
	qpath := func(path string, extra string) string {
		u := "/cp?path=" + url.QueryEscape(path)
		if extra != "" {
			u += "&" + extra
		}
		return u
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cp", strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing path put: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, qpath(dir+"/f", "mode=weird"), strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad mode: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, qpath(dir+"/f", "uid=bad"), strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad uid: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, qpath(dir+"/f", "gid=bad"), strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad gid: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, qpath(dir+"/f", "permissions=zz"), strings.NewReader("x"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad perms: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/cp", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("missing path get: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, qpath(dir, "mode=binary"), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("binary dir: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, qpath(dir+"/nope", "mode=weird"), nil)
	h.ServeHTTP(rr, req)
	// mode checked after Lstat for missing may 404 first
	if rr.Code != http.StatusNotFound && rr.Code != http.StatusBadRequest {
		t.Fatalf("bad get mode: %d", rr.Code)
	}

	// create file then bad mode
	f := filepath.Join(dir, "exists")
	if err := os.WriteFile(f, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, qpath(f, "mode=weird"), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad get mode file: %d", rr.Code)
	}
}

func TestHandleFSValidation(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	dir := t.TempDir()

	for _, path := range []string{"/fs/readdir", "/fs/stat"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("%s missing path: %d", path, rr.Code)
		}
	}

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/fs/mkdir", strings.NewReader("{"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad json mkdir: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/fs/mkdir", strings.NewReader(`{"path":""}`))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("empty path mkdir: %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	b, _ := json.Marshal(map[string]string{"path": filepath.Join(dir, "m"), "mode": "bad"})
	req = httptest.NewRequest(http.MethodPost, "/fs/mkdir", bytes.NewReader(b))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("bad mode mkdir: %d", rr.Code)
	}

	// non-recursive mkdir conflict
	leaf := filepath.Join(dir, "leaf")
	if err := os.Mkdir(leaf, 0o755); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	b, _ = json.Marshal(map[string]string{"path": leaf})
	req = httptest.NewRequest(http.MethodPost, "/fs/mkdir", bytes.NewReader(b))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusConflict {
		t.Fatalf("conflict: %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/fs/remove", nil))
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("remove missing path: %d", rr.Code)
	}
}

func TestMaterializeSecretValidation(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()

	cases := []struct {
		body string
		code int
	}{
		{"{", http.StatusBadRequest},
		{`{}`, http.StatusBadRequest},
		{`{"name":"x"}`, http.StatusBadRequest},
		{`{"name":"x","data_base64":"!!!"}`, http.StatusBadRequest},
		{`{"name":"x","data_base64":"YQ==","mode":"bad"}`, http.StatusBadRequest},
	}
	for _, tc := range cases {
		rr := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/secrets/materialize", strings.NewReader(tc.body))
		h.ServeHTTP(rr, req)
		if rr.Code != tc.code {
			t.Fatalf("body %s: got %d want %d (%s)", tc.body, rr.Code, tc.code, rr.Body.String())
		}
	}

	// default path under temp — override by providing path
	dir := t.TempDir()
	path := filepath.Join(dir, "sec")
	rr := httptest.NewRecorder()
	payload, _ := json.Marshal(map[string]string{
		"name":        "n",
		"data_base64": "YQ==",
		"path":        path,
	})
	req := httptest.NewRequest(http.MethodPost, "/secrets/materialize", bytes.NewReader(payload))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("ok materialize: %d %s", rr.Code, rr.Body.String())
	}
}

func TestExecBufferedWithCwd(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	dir := t.TempDir()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/exec?cmd=pwd&cwd="+url.QueryEscape(dir), nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), dir) {
		t.Fatalf("cwd not used: %s", rr.Body.String())
	}
}

func TestClientLongHTTPAndErrors(t *testing.T) {
	t.Parallel()
	c := &Client{BaseURL: "http://example/"}
	if c.http() == nil {
		t.Fatal()
	}
	// clone with timeout → longHTTP zeros it
	c.HTTP = &http.Client{Timeout: 5 * time.Second}
	lh := c.longHTTP()
	if lh.Timeout != 0 {
		t.Fatalf("timeout %v", lh.Timeout)
	}
	// already zero
	c.HTTP = &http.Client{Timeout: 0}
	if c.longHTTP() != c.HTTP {
		t.Fatal("should reuse zero-timeout client")
	}
	if c.base() != "http://example" {
		t.Fatalf("base %q", c.base())
	}
}

func TestWaitNilClient(t *testing.T) {
	t.Parallel()
	if err := Wait(context.Background(), nil); err == nil {
		t.Fatal("expected nil client error")
	}
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
	// server returns errors
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
		// decode may succeed with error field
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

func TestExecStreamOnFrameErrorAndNoExit(t *testing.T) {
	t.Parallel()
	// frames without exit
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

	// onFrame error
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

	// error frame empty message
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

	// blank lines + bad json
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

func TestPutBinaryWithUIDGIDQuery(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	path := filepath.Join(t.TempDir(), "f.txt")
	rr := httptest.NewRecorder()
	u := "/cp?path=" + url.QueryEscape(path) + "&uid=501&gid=20&permissions=0640"
	req := httptest.NewRequest(http.MethodPost, u, strings.NewReader("body"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	got, err := os.ReadFile(path)
	if err != nil || string(got) != "body" {
		t.Fatalf("%q %v", got, err)
	}
}

func TestWriteNDJSONErrorAndWriteJSON(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	writeNDJSONError(rr, "boom")
	if !strings.Contains(rr.Body.String(), "boom") {
		t.Fatal(rr.Body.String())
	}
	rr = httptest.NewRecorder()
	writeJSON(rr, http.StatusOK, map[string]string{"a": "b"})
	if rr.Code != 200 || !strings.Contains(rr.Body.String(), "a") {
		t.Fatal(rr.Body.String())
	}
}

func TestPutTarIllegalPath(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	err := putTar(t.TempDir(), bytes.NewReader(buf.Bytes()), nil, nil)
	if err == nil {
		t.Fatal("expected illegal path")
	}
}

func TestReadMeminfoMemFreeFallback(t *testing.T) {
	dir := t.TempDir()
	// only MemFree, no MemAvailable
	mem := "MemTotal: 1000 kB\nMemFree: 400 kB\n"
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte(mem), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "uptime"), []byte("1.0 2.0\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loadavg"), []byte("0.1 0.1 0.1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := SetProcPathsForTest(
		filepath.Join(dir, "uptime"),
		filepath.Join(dir, "meminfo"),
		filepath.Join(dir, "loadavg"),
	)
	t.Cleanup(restore)
	st := CollectStats()
	if st.MemAvail != 400*1024 {
		t.Fatalf("MemAvail from Free: %d", st.MemAvail)
	}
}

func TestReadProcEdgeErrors(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "uptime"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "loadavg"), []byte("\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "meminfo"), []byte("junk\nMemTotal: notnum kB\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	restore := SetProcPathsForTest(
		filepath.Join(dir, "uptime"),
		filepath.Join(dir, "meminfo"),
		filepath.Join(dir, "loadavg"),
	)
	t.Cleanup(restore)
	// CollectStats ignores read errors
	_ = CollectStats()
	// direct errors
	var st Stats
	if err := readUptime(&st); err == nil {
		t.Fatal("empty uptime")
	}
	if err := readLoadavg(&st); err == nil {
		t.Fatal("empty loadavg")
	}
}
