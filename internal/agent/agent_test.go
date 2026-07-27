package agent_test

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/agent"
)

func startTestServer(t *testing.T) *agent.Client {
	t.Helper()
	srv := agent.NewServer("127.0.0.1:0", nil)
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

	c := &agent.Client{
		BaseURL: base,
		HTTP:    &http.Client{Timeout: 5 * time.Second},
	}

	// Confirm health before returning.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := agent.Wait(ctx, c); err != nil {
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
	if h.AgentVersion != agent.Version {
		t.Errorf("AgentVersion = %q, want %q", h.AgentVersion, agent.Version)
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
	res, err := c.ExecBufferedOpts(ctx, agent.ExecOpts{Cmd: ""})
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
	code, err := c.ExecStream(ctx, agent.ExecOpts{Cmd: "echo", Args: []string{"hello"}}, func(f agent.ExecFrame) error {
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
	code, err := c.ExecStream(ctx, agent.ExecOpts{Cmd: "/bin/sh", Args: []string{"-c", "exit 42"}}, func(f agent.ExecFrame) error {
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
	_, err := c.ExecStream(ctx, agent.ExecOpts{Cmd: "/nonexistent/grain-agent-cmd-xyz"}, func(f agent.ExecFrame) error {
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

	if err := c.PutFile(ctx, guestPath, bytes.NewReader(payload), int64(len(payload)), agent.CPOpts{
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
	if err := c.PutFile(ctx, child, strings.NewReader("data"), 4, agent.CPOpts{}); err != nil {
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

func TestWaitSucceeds(t *testing.T) {
	c := startTestServer(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := agent.Wait(ctx, c); err != nil {
		t.Fatalf("Wait: %v", err)
	}
}

func TestWaitTimeout(t *testing.T) {
	c := &agent.Client{
		BaseURL: "http://127.0.0.1:1", // nothing listening
		HTTP:    &http.Client{Timeout: 100 * time.Millisecond},
	}
	ctx, cancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer cancel()
	err := agent.Wait(ctx, c)
	if err == nil {
		t.Fatal("expected Wait timeout error")
	}
}

func TestVersionConstant(t *testing.T) {
	if agent.Version != "0.2.0" {
		t.Errorf("Version = %q, want 0.2.0", agent.Version)
	}
	if agent.DefaultListen != ":7475" {
		t.Errorf("DefaultListen = %q, want :7475", agent.DefaultListen)
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
	restore := agent.SetProcPathsForTest(
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
	res, err := c.MaterializeSecret(ctx, agent.MaterializeSecretRequest{
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
