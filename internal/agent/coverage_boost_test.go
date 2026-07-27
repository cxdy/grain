package agent

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestServerListenBadAddrAndAddrString(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	if s.AddrString() == "" {
		t.Fatal("addr")
	}
	s2 := NewServer("256.256.256.256:9999", nil)
	if err := s2.ListenAndServe(); err == nil {
		t.Fatal("expected listen error")
	}
}

func TestUserdataRanEnvAndHealthHEAD(t *testing.T) {
	t.Setenv("GRAIN_USERDATA_RAN", "true")
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodHead, "/health", nil)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("head %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/health", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("get %d", rr.Code)
	}
	var health Health
	if err := json.Unmarshal(rr.Body.Bytes(), &health); err != nil {
		t.Fatal(err)
	}
	if !health.UserdataRan {
		t.Fatal("expected userdata ran from env")
	}
}

func TestExecBufferedMissingCmdAndTimeout(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/exec?cmd=grain-no-such-cmd-xyz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("%d", rr.Code)
	}
	var res ExecResult
	_ = json.Unmarshal(rr.Body.Bytes(), &res)
	if res.ExitCode == 0 && res.Error == "" {
		t.Fatalf("%+v", res)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/exec?cmd=false", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/exec?cmd=false&buffered=false", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("%d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/exec?cmd=/no/such/binary/grain&buffered=false", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("%d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "error") {
		t.Log(rr.Body.String())
	}

	dir := t.TempDir()
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/exec?cmd=pwd&cwd="+dir, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
}

func TestCPPutGetTarAndBinaryRoundTrip(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	dir := t.TempDir()
	path := filepath.Join(dir, "f.txt")

	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/cp?path="+path+"&mode=binary&permissions=0644", strings.NewReader("data"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("put %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/cp?path="+path+"&mode=binary", nil))
	if rr.Code != http.StatusOK || rr.Body.String() != "data" {
		t.Fatalf("get %d %q", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/cp?path="+path, strings.NewReader("data2"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("put2 %d", rr.Code)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "a.txt", Mode: 0o644, Size: 3}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("abc")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	dest := filepath.Join(dir, "tree")
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/cp?path="+dest+"&mode=tar", &buf)
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("puttar %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/cp?path="+dir+"&mode=tar", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("gettar %d %s", rr.Code, rr.Body.String())
	}
	if rr.Body.Len() == 0 {
		t.Fatal("empty tar")
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/cp?path="+filepath.Join(dir, "nope")+"&mode=binary", nil))
	if rr.Code != http.StatusNotFound {
		t.Fatalf("%d", rr.Code)
	}
}

func TestMaterializeSecretFull(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	dir := t.TempDir()
	path := filepath.Join(dir, "sec")
	payload := base64.StdEncoding.EncodeToString([]byte("secret-data"))

	body, _ := json.Marshal(MaterializeSecretRequest{
		Name:       "k",
		DataBase64: payload,
		Path:       path,
		Mode:       "0600",
	})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/secrets/materialize", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
	}
	b, err := os.ReadFile(path)
	if err != nil || string(b) != "secret-data" {
		t.Fatalf("%q %v", b, err)
	}

	body2, _ := json.Marshal(MaterializeSecretRequest{
		Name:       "k2",
		DataBase64: payload,
	})
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/secrets/materialize", bytes.NewReader(body2))
	h.ServeHTTP(rr, req)
	t.Logf("default path materialize: %d", rr.Code)

	raw := base64.RawStdEncoding.EncodeToString([]byte("x"))
	body3, _ := json.Marshal(MaterializeSecretRequest{
		Name: "k3", DataBase64: raw, Path: filepath.Join(dir, "raw"),
	})
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodPost, "/secrets/materialize", bytes.NewReader(body3)))
	if rr.Code != http.StatusOK {
		t.Fatalf("raw %d %s", rr.Code, rr.Body.String())
	}
}

func TestFSOpsRecursiveAndErrors(t *testing.T) {
	s := NewServer("127.0.0.1:0", nil)
	h := s.Handler()
	dir := t.TempDir()
	sub := filepath.Join(dir, "a", "b")
	body, _ := json.Marshal(MkdirRequest{Path: sub, Recursive: true, Mode: "0755"})
	rr := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/fs/mkdir", bytes.NewReader(body))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("mkdir %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/fs/readdir?path="+dir, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("readdir %d", rr.Code)
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/fs/stat?path="+sub, nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("stat %d", rr.Code)
	}

	f := filepath.Join(sub, "f")
	if err := os.WriteFile(f, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/fs/remove?path="+filepath.Join(dir, "a")+"&recursive=true", nil))
	if rr.Code != http.StatusNoContent {
		t.Fatalf("rm %d %s", rr.Code, rr.Body.String())
	}

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodDelete, "/fs/remove?path="+filepath.Join(dir, "nope"), nil))
	t.Logf("rm missing %d", rr.Code)

	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/fs/readdir?path="+filepath.Join(dir, "nope"), nil))
	if rr.Code == http.StatusOK {
		t.Fatal("expected readdir error")
	}
}

func TestPutTarSymlinkAndDirEntries(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "subdir/", Mode: 0o755, Typeflag: tar.TypeDir}); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "subdir/f", Mode: 0o644, Size: 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("ok")); err != nil {
		t.Fatal(err)
	}
	if err := tw.WriteHeader(&tar.Header{Name: "link", Mode: 0o777, Typeflag: tar.TypeSymlink, Linkname: "subdir/f"}); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	if err := putTar(dest, &buf, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dest, "subdir", "f")); err != nil {
		t.Fatal(err)
	}
}

func TestSafeTarPathAndWriteTarFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if _, err := safeTarPath(dir, "../escape"); err == nil {
		t.Fatal("escape")
	}
	p, err := safeTarPath(dir, "ok.txt")
	if err != nil || !strings.HasPrefix(p, dir) {
		t.Fatalf("%s %v", p, err)
	}

	f := filepath.Join(dir, "one")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeTar(&buf, f); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty")
	}
}

func TestDecodeBase64Variants(t *testing.T) {
	t.Parallel()
	std := base64.StdEncoding.EncodeToString([]byte("hi"))
	b, err := decodeBase64(std + "\n")
	if err != nil || string(b) != "hi" {
		t.Fatalf("%q %v", b, err)
	}
	raw := base64.RawStdEncoding.EncodeToString([]byte("hi"))
	b, err = decodeBase64(raw)
	if err != nil || string(b) != "hi" {
		t.Fatalf("%q %v", b, err)
	}
	if _, err := decodeBase64("!!!!"); err == nil {
		t.Fatal("bad")
	}
}

func TestClientHTTPNilDefaultAndLongHTTP(t *testing.T) {
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
}

func TestClientExecStreamAndMaterialize(t *testing.T) {
	c := startAgentClient(t)
	ctx := context.Background()
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

	dir := t.TempDir()
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
	var out bytes.Buffer
	if err := c.GetTar(ctx, dest, &out); err != nil {
		t.Fatal(err)
	}
}

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
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			base = "http://" + addr
			break
		}
		time.Sleep(10 * time.Millisecond)
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

func TestApplyCredentialWithExecCmd(t *testing.T) {
	t.Parallel()
	uid := uint32(0)
	gid := uint32(0)
	applyCredential(exec.Command("true"), &uid, nil)
	applyCredential(exec.Command("true"), nil, &gid)
	applyCredential(exec.Command("true"), &uid, &gid)
	if os.Geteuid() != 0 {
		// non-root: SysProcAttr.Credential should remain unset
		cmd := exec.Command("true")
		applyCredential(cmd, &uid, &gid)
		if cmd.SysProcAttr != nil && cmd.SysProcAttr.Credential != nil {
			t.Fatal("unexpected credential on non-root")
		}
	}
}

func TestParseOctalModeEdges(t *testing.T) {
	t.Parallel()
	if _, err := parseOctalMode("xyz"); err == nil {
		t.Fatal("bad")
	}
	m, err := parseOctalMode("755")
	if err != nil || m == 0 {
		t.Fatalf("%v %v", m, err)
	}
	m, err = parseOctalMode("0644")
	if err != nil {
		t.Fatal(err)
	}
	_ = m
}

func TestWriteNDJSONErrorAndJSON(t *testing.T) {
	t.Parallel()
	rr := httptest.NewRecorder()
	writeNDJSONError(rr, "boom")
	if !strings.Contains(rr.Body.String(), "boom") {
		t.Fatalf("%s", rr.Body.String())
	}
	rr2 := httptest.NewRecorder()
	writeJSON(rr2, 200, map[string]string{"a": "b"})
	if rr2.Code != 200 {
		t.Fatal(rr2.Code)
	}
}

func TestCopyFileReplaceAndPutBinary(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	dst := filepath.Join(dir, "dst")
	if err := os.WriteFile(src, []byte("abc"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := copyFileReplace(src, dst, 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "abc" {
		t.Fatalf("%q %v", b, err)
	}
	// putBinary
	p := filepath.Join(dir, "sub", "f")
	if err := putBinary(p, strings.NewReader("zz"), 0o600, nil, nil); err != nil {
		t.Fatal(err)
	}
}
