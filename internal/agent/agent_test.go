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
)

func startTestServer(t *testing.T) *Client {
	t.Helper()
	srv := NewServer("127.0.0.1:0", nil)
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.ListenAndServe()
	}()

	// Wait briefly for listen.
	deadline := time.Now().Add(2 * time.Second)
	var base string
	for time.Now().Before(deadline) {
		addr := srv.AddrString()
		if addr != "" && addr != "127.0.0.1:0" && !strings.HasSuffix(addr, ":0") {
			base = "http://" + addr
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if base == "" {
		t.Fatal("server did not bind a port")
	}

	c := &Client{
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}

	// Confirm health before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := Wait(ctx, c); err != nil {
		t.Fatalf("wait for test server: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = srv.Shutdown(ctx)
		select {
		case <-errCh:
		case <-time.After(2 * time.Second):
		}
	})

	return c
}

func TestHealth(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	h, err := c.Health(ctx)
	if err != nil {
		t.Fatalf("Health: %v", err)
	}
	if h.AgentVersion != Version {
		t.Errorf("AgentVersion = %q, want %q", h.AgentVersion, Version)
	}
	if h.Hostname == "" {
		t.Error("Hostname empty")
	}
	if h.AgentUptime < 0 {
		t.Errorf("AgentUptime = %d, want >= 0", h.AgentUptime)
	}

	if err := c.HeadHealth(ctx); err != nil {
		t.Fatalf("HeadHealth: %v", err)
	}
}

func TestExecBufferedEcho(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	res, err := c.ExecBuffered(ctx, "echo", "hello")
	if err != nil {
		t.Fatalf("ExecBuffered: %v", err)
	}
	if res.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0; stderr=%q error=%q", res.ExitCode, res.Stderr, res.Error)
	}
	out := strings.TrimSpace(res.Stdout)
	if out != "hello" {
		t.Errorf("Stdout = %q, want %q", out, "hello")
	}
}

func TestExecBufferedNonZero(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	// /bin/sh -c "exit 42"
	res, err := c.ExecBuffered(ctx, "/bin/sh", "-c", "exit 42")
	if err != nil {
		t.Fatalf("ExecBuffered: %v", err)
	}
	if res.ExitCode != 42 {
		t.Errorf("ExitCode = %d, want 42; stderr=%q error=%q", res.ExitCode, res.Stderr, res.Error)
	}
}

func TestExecMissingCmd(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	res, err := c.ExecBufferedOpts(ctx, ExecOpts{Cmd: ""})
	if err == nil {
		// Client rejects empty cmd before request.
		t.Fatal("expected error for empty cmd")
	}
	_ = res
}

func TestExecStreamEcho(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	var types []string
	var stdout strings.Builder
	code, err := c.ExecStream(ctx, ExecOpts{Cmd: "echo", Args: []string{"hello"}}, func(f ExecFrame) error {
		types = append(types, f.Type)
		if f.Type == "stdout" {
			stdout.WriteString(f.Data)
		}
		if f.Type == "started" && f.PID <= 0 {
			t.Errorf("started frame missing pid: %+v", f)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("ExecStream: %v", err)
	}
	if code != 0 {
		t.Errorf("exit code = %d, want 0", code)
	}
	if strings.TrimSpace(stdout.String()) != "hello" {
		t.Errorf("stdout = %q, want %q", stdout.String(), "hello")
	}
	// Expect started … exit; at least one stdout.
	if len(types) < 3 {
		t.Fatalf("frames %v: want at least started, stdout, exit", types)
	}
	if types[0] != "started" {
		t.Errorf("first frame = %q, want started", types[0])
	}
	if types[len(types)-1] != "exit" {
		t.Errorf("last frame = %q, want exit", types[len(types)-1])
	}
	foundStdout := false
	for _, typ := range types {
		if typ == "stdout" {
			foundStdout = true
		}
	}
	if !foundStdout {
		t.Errorf("frames %v: missing stdout", types)
	}
}

func TestExecStreamNonZero(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	code, err := c.ExecStream(ctx, ExecOpts{Cmd: "/bin/sh", Args: []string{"-c", "exit 42"}}, func(f ExecFrame) error {
		return nil
	})
	if err != nil {
		t.Fatalf("ExecStream: %v", err)
	}
	if code != 42 {
		t.Errorf("exit code = %d, want 42", code)
	}
}

func TestExecStreamStartError(t *testing.T) {
	c := startTestServer(t)

	ctx := context.Background()
	_, err := c.ExecStream(ctx, ExecOpts{Cmd: "/nonexistent/grain-agent-cmd-xyz"}, func(f ExecFrame) error {
		if f.Type != "error" {
			t.Errorf("expected error frame, got %q", f.Type)
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error from missing command")
	}
}

func TestPutGetFileRoundtrip(t *testing.T) {
	c := startTestServer(t)
	ctx := context.Background()

	dir := t.TempDir()
	guestPath := filepath.Join(dir, "sub", "hello.txt")
	payload := []byte("hello grain agent file copy\n")

	if err := c.PutFile(ctx, guestPath, bytes.NewReader(payload), int64(len(payload)), CPOpts{
		Mode: "0644",
	}); err != nil {
		t.Fatalf("PutFile: %v", err)
	}

	// Verify on disk.
	got, err := os.ReadFile(guestPath)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if !bytes.Equal(got, payload) {
		t.Errorf("disk content = %q, want %q", got, payload)
	}

	var buf bytes.Buffer
	if err := c.GetFile(ctx, guestPath, &buf); err != nil {
		t.Fatalf("GetFile: %v", err)
	}
	if !bytes.Equal(buf.Bytes(), payload) {
		t.Errorf("GetFile = %q, want %q", buf.Bytes(), payload)
	}
}

func TestPutTarGetTar(t *testing.T) {
	c := startTestServer(t)
	ctx := context.Background()

	// Build a small tar in memory: a.txt, nested/b.txt
	var tarBuf bytes.Buffer
	tw := tar.NewWriter(&tarBuf)
	files := map[string]string{
		"a.txt":        "alpha",
		"nested/b.txt": "bravo",
	}
	for name, content := range files {
		hdr := &tar.Header{
			Name: name,
			Mode: 0o644,
			Size: int64(len(content)),
		}
		if err := tw.WriteHeader(hdr); err != nil {
			t.Fatal(err)
		}
		if _, err := io.WriteString(tw, content); err != nil {
			t.Fatal(err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}

	dest := filepath.Join(t.TempDir(), "extract")
	if err := c.PutTar(ctx, dest, bytes.NewReader(tarBuf.Bytes())); err != nil {
		t.Fatalf("PutTar: %v", err)
	}

	// Files created on disk.
	for name, content := range files {
		p := filepath.Join(dest, name)
		got, err := os.ReadFile(p)
		if err != nil {
			t.Fatalf("read %s: %v", p, err)
		}
		if string(got) != content {
			t.Errorf("%s = %q, want %q", name, got, content)
		}
	}

	// GetTar of the directory and re-extract to verify.
	var outTar bytes.Buffer
	if err := c.GetTar(ctx, dest, &outTar); err != nil {
		t.Fatalf("GetTar: %v", err)
	}
	tr := tar.NewReader(&outTar)
	found := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read: %v", err)
		}
		if hdr.Typeflag == tar.TypeDir || strings.HasSuffix(hdr.Name, "/") {
			continue
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		found[filepath.ToSlash(hdr.Name)] = string(b)
	}
	for name, content := range files {
		if found[name] != content {
			t.Errorf("GetTar entry %s = %q, want %q (all=%v)", name, found[name], content, found)
		}
	}
}

func TestFSReadDirStatMkdirRemove(t *testing.T) {
	c := startTestServer(t)
	ctx := context.Background()

	root := t.TempDir()
	// Seed one file.
	if err := os.WriteFile(filepath.Join(root, "seed.txt"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Mkdir nested.
	nested := filepath.Join(root, "a", "b")
	if err := c.Mkdir(ctx, nested, true, "0755"); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	st, err := c.Stat(ctx, nested)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	if st.Type != "directory" {
		t.Errorf("Stat type = %q, want directory", st.Type)
	}
	if st.Name != "b" {
		t.Errorf("Stat name = %q, want b", st.Name)
	}

	// Put a file under nested and readdir.
	child := filepath.Join(nested, "c.txt")
	if err := c.PutFile(ctx, child, strings.NewReader("data"), 4, CPOpts{}); err != nil {
		t.Fatalf("PutFile: %v", err)
	}

	entries, err := c.ReadDir(ctx, nested)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name)
		if e.Name == "c.txt" {
			if e.Type != "file" {
				t.Errorf("c.txt type = %q, want file", e.Type)
			}
			if e.Size != 4 {
				t.Errorf("c.txt size = %d, want 4", e.Size)
			}
		}
	}
	if !contains(names, "c.txt") {
		t.Errorf("ReadDir names = %v, want c.txt", names)
	}

	// Stat file.
	fst, err := c.Stat(ctx, child)
	if err != nil {
		t.Fatalf("Stat file: %v", err)
	}
	if fst.Name != "c.txt" || fst.Type != "file" {
		t.Errorf("Stat file = %+v", fst)
	}

	// Recursive remove of a/
	if err := c.Remove(ctx, filepath.Join(root, "a"), true); err != nil {
		t.Fatalf("Remove recursive: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "a")); !os.IsNotExist(err) {
		t.Errorf("expected a/ removed, stat err=%v", err)
	}

	// seed.txt still present
	entries, err = c.ReadDir(ctx, root)
	if err != nil {
		t.Fatalf("ReadDir root: %v", err)
	}
	names = nil
	for _, e := range entries {
		names = append(names, e.Name)
	}
	if !contains(names, "seed.txt") {
		t.Errorf("ReadDir root = %v, want seed.txt", names)
	}
}

func TestNotFoundPaths(t *testing.T) {
	c := startTestServer(t)
	ctx := context.Background()

	missing := filepath.Join(t.TempDir(), "no-such-path-xyz")

	if _, err := c.Stat(ctx, missing); err == nil {
		t.Error("Stat: expected not found")
	}
	if _, err := c.ReadDir(ctx, missing); err == nil {
		t.Error("ReadDir: expected not found")
	}
	var buf bytes.Buffer
	if err := c.GetFile(ctx, missing, &buf); err == nil {
		t.Error("GetFile: expected not found")
	}
	if err := c.GetTar(ctx, missing, &buf); err == nil {
		t.Error("GetTar: expected not found")
	}
	if err := c.Remove(ctx, missing, false); err == nil {
		t.Error("Remove: expected not found")
	}
}

func TestVersionConstant(t *testing.T) {
	if Version != "0.3.0" {
		t.Errorf("Version = %q, want 0.3.0", Version)
	}
	if DefaultListen != ":7475" {
		t.Errorf("DefaultListen = %q, want :7475", DefaultListen)
	}
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestStatsEndpoint(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "uptime"), []byte("10.0 20.0\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "meminfo"), []byte("MemTotal: 1024 kB\nMemAvailable: 512 kB\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "loadavg"), []byte("1.5 1.0 0.5 1/1 1\n"), 0o644)
	restore := SetProcPathsForTest(
		filepath.Join(dir, "uptime"),
		filepath.Join(dir, "meminfo"),
		filepath.Join(dir, "loadavg"),
	)
	t.Cleanup(restore)

	c := startTestServer(t)
	st, err := c.Stats(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if st.UptimeSec != 10.0 {
		t.Fatalf("uptime %v", st.UptimeSec)
	}
	if st.MemTotal != 1024*1024 {
		t.Fatalf("mem total %d", st.MemTotal)
	}
	if st.Load1 != 1.5 {
		t.Fatalf("load1 %v", st.Load1)
	}
}

func TestMaterializeSecret(t *testing.T) {
	c := startTestServer(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "mysecret")
	ctx := context.Background()
	res, err := c.MaterializeSecret(ctx, MaterializeSecretRequest{
		Name:       "mysecret",
		DataBase64: "aGVsbG8=", // hello
		Path:       path,
		Mode:       "0600",
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Path != path {
		t.Fatalf("path %q", res.Path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "hello" {
		t.Fatalf("content %q", b)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
}

// --- whitebox server / helper unit tests (merged from coverage/helpers/ownership) ---

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

	t.Setenv("GRAIN_USERDATA_RAN", "1")
	if !userdataRan() {
		t.Fatal("expected env override true")
	}
	t.Setenv("GRAIN_USERDATA_RAN", "")
	_ = userdataRan()
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
	m, err = parseOctalMode("755")
	if err != nil || m == 0 {
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
	if _, err := decodeBase64("!!!!"); err == nil {
		t.Fatal("bad")
	}
}

func TestSafeTarPathAndWriteTar(t *testing.T) {
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

	f := filepath.Join(dest, "one")
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

func TestPutTarSymlinkDirAndIllegal(t *testing.T) {
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

	// illegal path
	var bad bytes.Buffer
	tw2 := tar.NewWriter(&bad)
	if err := tw2.WriteHeader(&tar.Header{Name: "../evil", Mode: 0o644, Size: 1, Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw2.Write([]byte("x")); err != nil {
		t.Fatal(err)
	}
	_ = tw2.Close()
	if err := putTar(t.TempDir(), bytes.NewReader(bad.Bytes()), nil, nil); err == nil {
		t.Fatal("expected illegal path")
	}
}

func TestSafeTarLinkname(t *testing.T) {
	t.Parallel()
	ok := []string{"a.txt", "sub/file", "./rel", "foo/bar/baz"}
	for _, s := range ok {
		if err := safeTarLinkname(s); err != nil {
			t.Fatalf("safeTarLinkname(%q): %v", s, err)
		}
	}
	bad := []string{
		"",
		"/etc/passwd",
		"/tmp",
		"../escape",
		"a/../../escape",
		"foo/../../../etc/passwd",
		"..",
		`\Windows\System32`,
	}
	for _, s := range bad {
		if err := safeTarLinkname(s); err == nil {
			t.Fatalf("safeTarLinkname(%q): expected error", s)
		}
	}
}

func TestPutTarRejectsEscapingSymlink(t *testing.T) {
	t.Parallel()

	// Absolute symlink target must be rejected (classic escape).
	var absBuf bytes.Buffer
	tw := tar.NewWriter(&absBuf)
	if err := tw.WriteHeader(&tar.Header{Name: "evil", Typeflag: tar.TypeSymlink, Linkname: "/tmp", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	dest := t.TempDir()
	if err := putTar(dest, bytes.NewReader(absBuf.Bytes()), nil, nil); err == nil {
		t.Fatal("expected absolute symlink target to be rejected")
	}
	if _, err := os.Lstat(filepath.Join(dest, "evil")); err == nil {
		t.Fatal("escaping symlink should not have been created")
	}

	// Symlink with ".." components must be rejected.
	var dotBuf bytes.Buffer
	tw2 := tar.NewWriter(&dotBuf)
	if err := tw2.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "../../outside", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	if err := tw2.Close(); err != nil {
		t.Fatal(err)
	}
	if err := putTar(t.TempDir(), bytes.NewReader(dotBuf.Bytes()), nil, nil); err == nil {
		t.Fatal("expected .. symlink target to be rejected")
	}

	// Escape via intermediate absolute symlink then write under it: first entry fails.
	var chain bytes.Buffer
	tw3 := tar.NewWriter(&chain)
	if err := tw3.WriteHeader(&tar.Header{Name: "out", Typeflag: tar.TypeSymlink, Linkname: "/etc", Mode: 0o777}); err != nil {
		t.Fatal(err)
	}
	payload := "pwned"
	if err := tw3.WriteHeader(&tar.Header{Name: "out/passwd", Mode: 0o644, Size: int64(len(payload)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw3, payload); err != nil {
		t.Fatal(err)
	}
	if err := tw3.Close(); err != nil {
		t.Fatal(err)
	}
	if err := putTar(t.TempDir(), bytes.NewReader(chain.Bytes()), nil, nil); err == nil {
		t.Fatal("expected chained absolute symlink escape to fail")
	}
}

func TestPutTarRefusesWriteThroughSymlink(t *testing.T) {
	t.Parallel()
	dest := t.TempDir()
	secret := filepath.Join(dest, "secret.txt")
	if err := os.WriteFile(secret, []byte("keep-me"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Pre-create a relative (allowed) symlink that points at secret.txt.
	link := filepath.Join(dest, "alias")
	if err := os.Symlink("secret.txt", link); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	body := "overwrite"
	if err := tw.WriteHeader(&tar.Header{Name: "alias", Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(tw, body); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := putTar(dest, bytes.NewReader(buf.Bytes()), nil, nil); err == nil {
		t.Fatal("expected write-through symlink to be refused")
	}
	got, err := os.ReadFile(secret)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "keep-me" {
		t.Fatalf("secret overwritten via symlink: %q", got)
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

func TestCopyFileReplaceAndPutBinary(t *testing.T) {
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
	p := filepath.Join(dir, "sub", "f")
	if err := putBinary(p, strings.NewReader("zz"), 0o600, nil, nil); err != nil {
		t.Fatal(err)
	}
	u := uint32(0)
	g := uint32(0)
	if err := putBinary(filepath.Join(dir, "f2"), strings.NewReader("hi"), 0o644, &u, &g); err != nil {
		t.Fatal(err)
	}
}

func TestApplyOwnershipAndCredential(t *testing.T) {
	t.Parallel()
	if err := applyOwnership("/tmp", nil, nil); err != nil {
		t.Fatal(err)
	}
	u := uint32(0)
	g := uint32(0)
	if os.Geteuid() != 0 {
		if err := applyOwnership(t.TempDir(), &u, &g); err != nil {
			t.Fatal(err)
		}
		if err := applyOwnership(t.TempDir(), &u, nil); err != nil {
			t.Fatal(err)
		}
		if err := applyOwnership(t.TempDir(), nil, &g); err != nil {
			t.Fatal(err)
		}
	}
	uid := uint32(1)
	cmd := exec.Command("true")
	applyCredential(cmd, &uid, &uid)
	applyCredential(cmd, &uid, nil)
	applyCredential(cmd, nil, &uid)
	applyCredential(cmd, nil, nil)
	if os.Geteuid() != 0 && cmd.SysProcAttr != nil {
		t.Fatalf("expected no credential when non-root: %+v", cmd.SysProcAttr)
	}
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
	if rr.Code != http.StatusNotFound && rr.Code != http.StatusBadRequest {
		t.Fatalf("bad get mode: %d", rr.Code)
	}

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

	// uid/gid query on put
	path := filepath.Join(dir, "owned.txt")
	rr = httptest.NewRecorder()
	u := "/cp?path=" + url.QueryEscape(path) + "&uid=501&gid=20&permissions=0640"
	req = httptest.NewRequest(http.MethodPost, u, strings.NewReader("body"))
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusNoContent {
		t.Fatalf("%d %s", rr.Code, rr.Body.String())
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

func TestReadMeminfoMemFreeFallback(t *testing.T) {
	dir := t.TempDir()
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
	_ = CollectStats()
	var st Stats
	if err := readUptime(&st); err == nil {
		t.Fatal("empty uptime")
	}
	if err := readLoadavg(&st); err == nil {
		t.Fatal("empty loadavg")
	}
}
