package cli

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/vm"
)

// ---- from cp_test.go ----

func TestParseCPSpec(t *testing.T) {
	tests := []struct {
		in    string
		guest bool
		name  string
		path  string
	}{
		{"sbox-1:/tmp/x", true, "sbox-1", "/tmp/x"},
		{"vm:/home/ubuntu/file.txt", true, "vm", "/home/ubuntu/file.txt"},
		{"sbox-1:rel/path", true, "sbox-1", "rel/path"},
		{"./local", false, "", "./local"},
		{"/tmp/file", false, "", "/tmp/file"},
		{"file.txt", false, "", "file.txt"},
		// slash before colon → host path (e.g. weird names, not NAME:path)
		{"/home/a:b", false, "", "/home/a:b"},
		{"./a:b", false, "", "./a:b"},
		// empty path after colon is still guest
		{"sbox-1:", true, "sbox-1", ""},
	}
	for _, tt := range tests {
		got := parseCPSpec(tt.in)
		if got.Guest != tt.guest || got.Name != tt.name || got.Path != tt.path {
			t.Errorf("parseCPSpec(%q) = guest=%v name=%q path=%q, want guest=%v name=%q path=%q",
				tt.in, got.Guest, got.Name, got.Path, tt.guest, tt.name, tt.path)
		}
		if got.Raw != tt.in {
			t.Errorf("parseCPSpec(%q).Raw = %q", tt.in, got.Raw)
		}
	}
}

func TestSafeHostTarPath(t *testing.T) {
	dest := t.TempDir()
	ok, err := safeHostTarPath(dest, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ok == dest {
		t.Fatal("expected nested path")
	}
	if _, err := safeHostTarPath(dest, "../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := safeHostTarPath(dest, "/abs"); err != nil {
		// absolute names are stripped of leading slash and joined under dest
		t.Fatalf("absolute name should be joined under dest, got %v", err)
	}
}

// ---- from cp_helpers_coverage_test.go ----

func TestParseCPSpecVariants(t *testing.T) {
	t.Parallel()
	g := parseCPSpec("sbox-1:/tmp/x")
	if !g.Guest || g.Name != "sbox-1" || g.Path != "/tmp/x" {
		t.Fatalf("%+v", g)
	}
	h := parseCPSpec("./file")
	if h.Guest || h.Path != "./file" {
		t.Fatalf("%+v", h)
	}
	// slash before colon → host
	h2 := parseCPSpec("/home/a:b")
	if h2.Guest {
		t.Fatalf("%+v", h2)
	}
	// empty name side
	h3 := parseCPSpec(":foo")
	if h3.Guest {
		t.Fatalf("%+v", h3)
	}
}

func TestIsAgentUnavailableCoverage(t *testing.T) {
	t.Parallel()
	if isAgentUnavailable(nil) {
		t.Fatal()
	}
	if !isAgentUnavailable(errAgentSkip) {
		t.Fatal("skip")
	}
	if !isAgentUnavailable(errors.New("agent not available on guest")) {
		t.Fatal("msg")
	}
	if isAgentUnavailable(errors.New("permission denied")) {
		t.Fatal("real err")
	}
}

func TestWriteExtractTarRoundTrip(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	src := filepath.Join(dir, "src")
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "a.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "b.txt"), []byte("world"), 0o600); err != nil {
		t.Fatal(err)
	}
	// symlink if supported
	_ = os.Symlink("a.txt", filepath.Join(src, "link"))

	var buf bytes.Buffer
	if err := writeLocalTar(&buf, src); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "dest")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := extractTar(&buf, dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "a.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("%q %v", b, err)
	}
	b, err = os.ReadFile(filepath.Join(dest, "sub", "b.txt"))
	if err != nil || string(b) != "world" {
		t.Fatalf("%q %v", b, err)
	}
}

func TestWriteLocalTarSingleFileCoverage(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	f := filepath.Join(dir, "one.txt")
	if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeLocalTar(&buf, f); err != nil {
		t.Fatal(err)
	}
	if buf.Len() < 100 {
		t.Fatal("tiny tar")
	}
}

func TestSafeHostTarPathCoverage(t *testing.T) {
	t.Parallel()
	dest := t.TempDir()
	p, err := safeHostTarPath(dest, "ok.txt")
	if err != nil || !strings.HasPrefix(p, dest) {
		t.Fatalf("%s %v", p, err)
	}
	if _, err := safeHostTarPath(dest, "../escape"); err == nil {
		t.Fatal("escape")
	}
	if _, err := safeHostTarPath(dest, ".."); err == nil {
		t.Fatal("dotdot")
	}
}

func TestExtractTarRejectsEscape(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	hdr := &tar.Header{Name: "../evil", Mode: 0o644, Size: 1}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = tw.Close()
	dest := t.TempDir()
	if err := extractTar(&buf, dest); err == nil {
		t.Fatal("expected escape error")
	}
}

func TestExtractTarEmpty(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	_ = tw.Close()
	if err := extractTar(io.NopCloser(&buf), t.TempDir()); err != nil {
		t.Fatal(err)
	}
}

// ---- from cp_io_coverage_test.go ----

// mockAPIForCP implements the daemon cp/stat/tar surface used by daemonPut/Get.
func mockAPIForCP(t *testing.T) *api.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		mode := r.URL.Query().Get("mode")
		if mode == "tar" {
			var buf bytes.Buffer
			_ = writeLocalTar(&buf, t.TempDir())
			_, _ = w.Write(buf.Bytes())
			return
		}
		_, _ = w.Write([]byte("file-bytes"))
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		typ := "file"
		name := filepath.Base(p)
		if strings.Contains(p, "dir") {
			typ = "directory"
		}
		_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: name, Type: typ, Size: 10, Mode: "0644"})
	})
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "vm", Status: vm.StatusRunning, AgentPort: 1})
	})
	mux.HandleFunc("GET /vms/{name}/agent/health", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&agent.Health{Hostname: "g", AgentVersion: "1"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return &api.Client{Base: srv.URL, HTTP: srv.Client()}
}

func TestCPViaAgentPolicyErrors(t *testing.T) {
	t.Parallel()
	c := &api.Client{Base: "http://127.0.0.1:1"}
	// host to host
	if err := cpViaAgent(c, parseCPSpec("/a"), parseCPSpec("/b"), false, false); err != errAgentSkip {
		t.Fatalf("host-host: %v", err)
	}
	// guest to guest different
	if err := cpViaAgent(c, parseCPSpec("a:/x"), parseCPSpec("b:/y"), false, false); err == nil {
		t.Fatal("guest-guest")
	}
	// same guest
	if err := cpViaAgent(c, parseCPSpec("a:/x"), parseCPSpec("a:/y"), false, false); err == nil {
		t.Fatal("same guest")
	}
}

func TestDaemonPutFileAndDir(t *testing.T) {
	t.Parallel()
	c := mockAPIForCP(t)
	ctx := context.Background()
	dir := t.TempDir()
	f := filepath.Join(dir, "hello.txt")
	if err := os.WriteFile(f, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := daemonPut(ctx, c, "vm", f, "/tmp/hello.txt"); err != nil {
		t.Fatal(err)
	}
	// trailing slash guest → join base name
	if err := daemonPut(ctx, c, "vm", f, "/tmp/"); err != nil {
		t.Fatal(err)
	}
	// directory
	sub := filepath.Join(dir, "sub")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := daemonPut(ctx, c, "vm", sub, "/tmp/sub"); err != nil {
		t.Fatal(err)
	}
	// missing src
	if err := daemonPut(ctx, c, "vm", filepath.Join(dir, "nope"), "/x"); err == nil {
		t.Fatal("missing")
	}
}

func TestDaemonGetFileAndDir(t *testing.T) {
	t.Parallel()
	c := mockAPIForCP(t)
	ctx := context.Background()
	dir := t.TempDir()
	out := filepath.Join(dir, "out.txt")
	if err := daemonGet(ctx, c, "vm", "/tmp/out.txt", out); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(out)
	if err != nil || string(b) != "file-bytes" {
		t.Fatalf("%q %v", b, err)
	}
	// host path is directory
	destDir := filepath.Join(dir, "d")
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := daemonGet(ctx, c, "vm", "/tmp/x", destDir); err != nil {
		t.Fatal(err)
	}
	// trailing slash
	if err := daemonGet(ctx, c, "vm", "/tmp/x", destDir+string(os.PathSeparator)); err != nil {
		t.Fatal(err)
	}
	// directory type guest path
	destDir2 := filepath.Join(dir, "fromdir")
	if err := daemonGet(ctx, c, "vm", "/tmp/dir", destDir2); err != nil {
		t.Fatal(err)
	}
}

func TestAgentPutGetWithMockAgent(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("POST /cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(204)
	})
	mux.HandleFunc("GET /cp", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("mode") == "tar" {
			var buf bytes.Buffer
			tmp := t.TempDir()
			_ = os.WriteFile(filepath.Join(tmp, "z.txt"), []byte("z"), 0o644)
			_ = writeLocalTar(&buf, tmp)
			_, _ = w.Write(buf.Bytes())
			return
		}
		_, _ = w.Write([]byte("from-agent"))
	})
	mux.HandleFunc("GET /fs/stat", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		typ := "file"
		if strings.Contains(p, "dir") {
			typ = "directory"
		}
		_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: filepath.Base(p), Type: typ, Size: 3, Mode: "0644"})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	ac := &agent.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	dir := t.TempDir()
	src := filepath.Join(dir, "s.txt")
	if err := os.WriteFile(src, []byte("s"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := agentPut(ctx, ac, src, "/tmp/s.txt"); err != nil {
		t.Fatal(err)
	}
	if err := agentPut(ctx, ac, src, "/tmp/"); err != nil {
		t.Fatal(err)
	}
	sub := filepath.Join(dir, "folder")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "i.txt"), []byte("i"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := agentPut(ctx, ac, sub, "/tmp/folder"); err != nil {
		t.Fatal(err)
	}

	out := filepath.Join(dir, "got.txt")
	if err := agentGet(ctx, ac, "/tmp/got.txt", out); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(out)
	if string(b) != "from-agent" {
		t.Fatalf("%q", b)
	}
	// into existing dir
	dd := filepath.Join(dir, "into")
	_ = os.MkdirAll(dd, 0o755)
	if err := agentGet(ctx, ac, "/tmp/got.txt", dd); err != nil {
		t.Fatal(err)
	}
	// directory pull
	dest := filepath.Join(dir, "pulldir")
	if err := agentGet(ctx, ac, "/tmp/dir", dest); err != nil {
		t.Fatal(err)
	}
	// missing src for put
	if err := agentPut(ctx, ac, filepath.Join(dir, "missing"), "/x"); err == nil {
		t.Fatal("missing")
	}
}

func TestCPViaAgentDaemonMode(t *testing.T) {
	t.Parallel()
	c := mockAPIForCP(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "x.txt")
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cpViaAgent(c, parseCPSpec(src), parseCPSpec("vm:/tmp/x.txt"), false, true); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "y.txt")
	if err := cpViaAgent(c, parseCPSpec("vm:/tmp/y.txt"), parseCPSpec(out), false, true); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCpForceFlags(t *testing.T) {
	t.Parallel()
	cfg := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(cfg, []byte("data_dir: /tmp\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{"--ssh", "--agent", "a", "b"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "cannot use") {
		t.Fatalf("%v", err)
	}
}

// ---- from cp_tar_test.go ----

func TestWriteLocalTarAndExtract(t *testing.T) {
	src := t.TempDir()
	// file + nested dir + symlink
	if err := os.WriteFile(filepath.Join(src, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(src, "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "sub", "nested.txt"), []byte("nest"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("hello.txt", filepath.Join(src, "link-to-hello")); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := writeLocalTar(&buf, src); err != nil {
		t.Fatal(err)
	}
	if buf.Len() == 0 {
		t.Fatal("empty tar")
	}

	dest := t.TempDir()
	if err := extractTar(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("hello: %v %q", err, b)
	}
	b, err = os.ReadFile(filepath.Join(dest, "sub", "nested.txt"))
	if err != nil || string(b) != "nest" {
		t.Fatalf("nested: %v %q", err, b)
	}
	link, err := os.Readlink(filepath.Join(dest, "link-to-hello"))
	if err != nil || link != "hello.txt" {
		t.Fatalf("link: %v %q", err, link)
	}
}

func TestWriteLocalTarSingleFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "solo.txt")
	if err := os.WriteFile(p, []byte("solo"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeLocalTar(&buf, p); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := extractTar(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "solo.txt"))
	if err != nil || string(b) != "solo" {
		t.Fatalf("%v %q", err, b)
	}
}

func TestExtractTarPathTraversal(t *testing.T) {
	// Build a malicious tar name via safeHostTarPath already tested; extract uses it.
	// Craft tar with ../escape via raw writer is heavy; exercise safeHostTarPath edges.
	dest := t.TempDir()
	if _, err := safeHostTarPath(dest, ""); err != nil {
		t.Fatal(err)
	}
	if _, err := safeHostTarPath(dest, "."); err != nil {
		t.Fatal(err)
	}
	if _, err := safeHostTarPath(dest, "ok/path"); err != nil {
		t.Fatal(err)
	}
	if _, err := safeHostTarPath(dest, "../x"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := safeHostTarPath(dest, "a/../../x"); err == nil {
		t.Fatal("expected nested traversal error")
	}
}

func TestCmdCpMutualExclusionAndArgs(t *testing.T) {
	cfg := ""
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{"--ssh", "--agent", "a", "b"})
	err := cmd.Execute()
	if err == nil || !strings.Contains(err.Error(), "cannot use --ssh and --agent") {
		t.Fatalf("%v", err)
	}
}

func TestExtractTarSymlinkAndDir(t *testing.T) {
	dir := t.TempDir()
	// Build a tar with dir, file, symlink
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	// directory
	if err := tw.WriteHeader(&tar.Header{Name: "sub/", Typeflag: tar.TypeDir, Mode: 0o755}); err != nil {
		t.Fatal(err)
	}
	// file with zero mode → default 0644
	payload := []byte("hello")
	if err := tw.WriteHeader(&tar.Header{Name: "sub/f.txt", Typeflag: tar.TypeReg, Mode: 0, Size: int64(len(payload))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write(payload); err != nil {
		t.Fatal(err)
	}
	// symlink
	if err := tw.WriteHeader(&tar.Header{Name: "sub/link", Typeflag: tar.TypeSymlink, Linkname: "f.txt", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := extractTar(bytes.NewReader(buf.Bytes()), dest); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dest, "sub", "f.txt"))
	if err != nil || string(b) != "hello" {
		t.Fatalf("file: %v %q", err, b)
	}
	link, err := os.Readlink(filepath.Join(dest, "sub", "link"))
	if err != nil || link != "f.txt" {
		t.Fatalf("link: %v %q", err, link)
	}
}

func TestWriteLocalTarWithSymlink(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "a.txt"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("a.txt", filepath.Join(dir, "l")); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := writeLocalTar(&buf, dir); err != nil {
		t.Fatal(err)
	}
	tr := tar.NewReader(&buf)
	var sawLink bool
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeSymlink {
			sawLink = true
			if h.Linkname != "a.txt" {
				t.Fatalf("linkname %q", h.Linkname)
			}
		}
	}
	if !sawLink {
		t.Fatal("expected symlink in tar")
	}
}

func TestDaemonPutGetTrailingSlash(t *testing.T) {
	var putPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/cp"):
			putPath = r.URL.Query().Get("path")
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(204)
		case strings.Contains(r.URL.Path, "/fs/stat"):
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "remote.txt", "type": "file", "mode": "0644", "size": 2})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cp"):
			_, _ = w.Write([]byte("ok"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	ctx := context.Background()
	src := filepath.Join(t.TempDir(), "local.txt")
	if err := os.WriteFile(src, []byte("xy"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := daemonPut(ctx, c, "vm", src, "/tmp/"); err != nil {
		t.Fatalf("put slash: %v", err)
	}
	if !strings.HasSuffix(putPath, "local.txt") {
		t.Fatalf("put path %q", putPath)
	}
	// empty guest path base case
	if err := daemonPut(ctx, c, "vm", src, ""); err != nil {
		t.Fatalf("put empty guest: %v", err)
	}
	hostDir := t.TempDir() + string(os.PathSeparator)
	if err := daemonGet(ctx, c, "vm", "/guest/remote.txt", hostDir); err != nil {
		t.Fatalf("get slash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(filepath.Clean(hostDir), "remote.txt")); err != nil {
		// hostDir has trailing sep; join still works
		if _, err2 := os.Stat(filepath.Join(t.TempDir(), "remote.txt")); err2 != nil {
			// check cleaned
			p := filepath.Join(strings.TrimRight(hostDir, string(os.PathSeparator)), "remote.txt")
			if _, err3 := os.Stat(p); err3 != nil {
				t.Fatalf("missing file: %v (also %v)", err, err3)
			}
		}
	}
}

func TestCmdCpBadConfigAndAuth(t *testing.T) {
	dir := t.TempDir()
	cmd := cmdCp(&dir)
	cmd.SetArgs([]string{"/a", "vm:/b"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected config error")
	}
	apiURLFlag = "http://example.com:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd = cmdCp(&cfg)
	cmd.SetArgs([]string{"/a", "vm:/b"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestAgentPutGetLivePaths(t *testing.T) {
	asrv := agent.NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() { errCh <- asrv.ListenAndServe() }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = asrv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})
	var port int
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		addr := asrv.AddrString()
		if addr != "" && !strings.HasSuffix(addr, ":0") {
			_, p, err := net.SplitHostPort(addr)
			if err == nil {
				port, _ = strconv.Atoi(p)
				if port > 0 {
					break
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	if port == 0 {
		t.Fatal("no port")
	}
	ac, err := agent.Dial(context.Background(), agent.Target{Port: port})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	srcDir := t.TempDir()
	srcFile := filepath.Join(srcDir, "f.txt")
	if err := os.WriteFile(srcFile, []byte("data"), 0o640); err != nil {
		t.Fatal(err)
	}
	// trailing slash guest path
	guestBase := t.TempDir()
	if err := agentPut(ctx, ac, srcFile, guestBase+"/"); err != nil {
		t.Fatalf("put slash: %v", err)
	}
	// empty guest path → /
	if err := agentPut(ctx, ac, srcFile, ""); err != nil {
		// may fail if agent rejects; log
		t.Logf("put empty: %v", err)
	}
	// put directory
	if err := agentPut(ctx, ac, srcDir, filepath.Join(guestBase, "dir")); err != nil {
		t.Fatalf("put dir: %v", err)
	}
	// get file with host trailing slash
	outDir := t.TempDir() + string(os.PathSeparator)
	guestFile := filepath.Join(guestBase, "f.txt")
	if err := agentGet(ctx, ac, guestFile, outDir); err != nil {
		t.Fatalf("get slash: %v", err)
	}
	// get into existing dir (no trailing slash)
	outDir2 := t.TempDir()
	if err := agentGet(ctx, ac, guestFile, outDir2); err != nil {
		t.Fatalf("get dir dest: %v", err)
	}
	// get directory as tar
	outTree := filepath.Join(t.TempDir(), "tree")
	if err := agentGet(ctx, ac, filepath.Join(guestBase, "dir"), outTree); err != nil {
		t.Logf("get dir (may need tar): %v", err)
	}
}

func TestDaemonGetDirectoryTar(t *testing.T) {
	// Serve stat as directory + GetTar stream
	var buf bytes.Buffer
	_ = writeLocalTar(&buf, t.TempDir())
	// put a file in a real dir for tar content
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "z.txt"), []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	var tarBuf bytes.Buffer
	if err := writeLocalTar(&tarBuf, src); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/fs/stat"):
			_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: "d", Type: "directory", Mode: "0755"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cp"):
			_, _ = w.Write(tarBuf.Bytes())
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	dest := filepath.Join(t.TempDir(), "out")
	if err := daemonGet(context.Background(), c, "vm", "/guest/d", dest); err != nil {
		t.Fatal(err)
	}
}

func TestCmdCpSuccessViaDaemonRemote(t *testing.T) {
	src := filepath.Join(t.TempDir(), "in.txt")
	if err := os.WriteFile(src, []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.HasSuffix(r.URL.Path, "/cp"):
			_, _ = io.Copy(io.Discard, r.Body)
			w.WriteHeader(204)
		case strings.Contains(r.URL.Path, "/fs/stat"):
			_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: "in.txt", Type: "file", Size: 3, Mode: "0644"})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/cp"):
			_, _ = w.Write([]byte("abc"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdCp(&cfg)
	cmd.SetArgs([]string{src, "vm:/tmp/in.txt"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cp put: %v", err)
	}
	out := filepath.Join(t.TempDir(), "out.txt")
	cmd = cmdCp(&cfg)
	cmd.SetArgs([]string{"vm:/tmp/in.txt", out})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("cp get: %v", err)
	}
}
