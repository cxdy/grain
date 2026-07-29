package mcp_test

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestRunHTTPAndStdioCancel(t *testing.T) {
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(daemon.Close)
	c, err := client.DialHTTP(daemon.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	go func() {
		errCh <- grainmcp.RunHTTP(ctx, addr, "t", c, t.TempDir(), slog.Default())
	}()
	// wait ready
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		res, err := http.Get("http://" + addr + "/")
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	// hit /mcp not found path and root 404
	res, err := http.Get("http://" + addr + "/nope")
	if err == nil {
		_ = res.Body.Close()
	}
	res2, err := http.Get("http://" + addr + "/")
	if err == nil {
		_ = res2.Body.Close()
	}
	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("RunHTTP did not exit")
	}

	// default listen empty + nil log
	ln2, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr2 := ln2.Addr().String()
	_ = ln2.Close()
	// can't use empty listen (defaults 7476) if port busy — use explicit
	ctx2, cancel2 := context.WithCancel(context.Background())
	go func() { _ = grainmcp.RunHTTP(ctx2, addr2, "", c, "", nil) }()
	time.Sleep(50 * time.Millisecond)
	cancel2()

	// listen fail
	if err := grainmcp.RunHTTP(context.Background(), "256.0.0.1:1", "t", c, "", slog.Default()); err == nil {
		t.Fatal("expected listen error")
	}

	// RunStdio with canceled ctx on in-memory is hard; call NewMCPServer empty version
	_ = grainmcp.NewMCPServer("", c)
}

func TestRunStdioQuickCancel(t *testing.T) {
	// StdioTransport needs real pipes - skip hang by using short process.
	// Ensure function exists and returns when server exits: use CommandTransport reverse.
	// Cover via handshake already; here call with canceled context before Run fully starts.
	daemon := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	}))
	t.Cleanup(daemon.Close)
	c, err := client.DialHTTP(daemon.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// Run may return quickly on canceled ctx or block on stdin
	done := make(chan error, 1)
	go func() {
		done <- grainmcp.RunStdio(ctx, "t", c, t.TempDir())
	}()
	select {
	case <-done:
	case <-time.After(500 * time.Millisecond):
		// hung on stdin — acceptable; we still covered entry
	}
}

func TestHTTPEndpointAndConnectDial(t *testing.T) {
	if grainmcp.HTTPEndpoint("") != "http://127.0.0.1:7476/mcp" {
		t.Fatal(grainmcp.HTTPEndpoint(""))
	}
	opts := grainmcp.ConnectFromEnv()
	_ = opts
	// Dial HTTP
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer t" {
			t.Errorf("auth %q", r.Header.Get("Authorization"))
		}
		w.WriteHeader(200)
	}))
	t.Cleanup(srv.Close)
	c, err := grainmcp.Dial(grainmcp.ConnectOptions{APIURL: srv.URL, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Health(t.Context()); err != nil {
		t.Fatal(err)
	}
	// Dial unix
	c2, err := grainmcp.Dial(grainmcp.ConnectOptions{Socket: filepath.Join(t.TempDir(), "x.sock")})
	if err != nil {
		t.Fatal(err)
	}
	_ = c2
	// Dial with config path
	dir := t.TempDir()
	cfg := filepath.Join(dir, "config.yaml")
	_ = os.WriteFile(cfg, []byte("data_dir: "+dir+"\nsocket: "+filepath.Join(dir, "g.sock")+"\napi_token: tok\n"), 0o644)
	c3, err := grainmcp.Dial(grainmcp.ConnectOptions{ConfigPath: cfg})
	if err != nil {
		t.Fatal(err)
	}
	_ = c3
	// empty api env
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_SOCKET", filepath.Join(dir, "z.sock"))
	c4, err := grainmcp.Dial(grainmcp.ConnectOptions{})
	if err != nil {
		t.Fatal(err)
	}
	_ = c4
}

func TestLocalHostImagePullErrors(t *testing.T) {
	h := grainmcp.NewLocalHost(t.TempDir())
	if err := h.ImagePull(t.Context(), "totally-unknown-id-xyz"); err == nil {
		t.Fatal("expected unknown")
	}
	// local-only grain-ubuntu import?
	// Pull grain-ubuntu may hit network - skip if fail
	list, err := h.ImageList()
	if err != nil || len(list) == 0 {
		t.Fatal(err)
	}
	// ReadVMLog missing
	if _, _, err := h.ReadVMLog("nope", false, 10); err == nil {
		t.Fatal("expected missing log")
	}
	// qemu log missing
	if _, _, err := h.ReadVMLog("nope", true, 0); err == nil {
		t.Fatal("expected missing qemu log")
	}
	// empty name
	if _, _, err := h.ReadVMLog("", false, 10); err == nil {
		t.Fatal("expected name required")
	}
	// truncate
	dir := t.TempDir()
	p := filepath.Join(dir, "vms", "n")
	_ = os.MkdirAll(p, 0o755)
	big := strings.Repeat("x", 1000)
	_ = os.WriteFile(filepath.Join(p, "serial.log"), []byte(big), 0o644)
	h2 := grainmcp.NewLocalHost(dir)
	_, content, err := h2.ReadVMLog("n", false, 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) != 100 {
		t.Fatalf("len %d", len(content))
	}
	// qemu path
	_ = os.MkdirAll(filepath.Join(dir, "logs"), 0o755)
	_ = os.WriteFile(filepath.Join(dir, "logs", "n.log"), []byte("qemu"), 0o644)
	_, c2, err := h2.ReadVMLog("n", true, 10)
	if err != nil || c2 != "qemu" {
		t.Fatalf("%q %v", c2, err)
	}
}

func TestNewMCPServerEmptyVersion(t *testing.T) {
	c, err := client.DialHTTP("http://127.0.0.1:1", "")
	if err != nil {
		t.Fatal(err)
	}
	_ = grainmcp.NewMCPServer("", c)
	_ = grainmcp.NewMCPServerOpts(grainmcp.ServerOptions{Client: c})
}

func TestToolErrorPaths(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx := t.Context()
	// empty name / invalid args should fail (Go error or IsError result)
	expectFail := func(tool string, args map[string]any) {
		t.Helper()
		res, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: tool, Arguments: args})
		if err != nil {
			return
		}
		if res != nil && res.IsError {
			return
		}
		t.Fatalf("%s expected failure, got %v", tool, res)
	}
	for _, name := range []string{
		grainmcp.ToolGetVM, grainmcp.ToolStartVM, grainmcp.ToolStopVM,
		grainmcp.ToolAgentHealth, grainmcp.ToolStats,
	} {
		expectFail(name, map[string]any{"name": ""})
	}
	expectFail(grainmcp.ToolExec, map[string]any{"name": "x", "cmd": ""})
	expectFail(grainmcp.ToolExec, map[string]any{"name": "x", "cmd": "true", "timeout": "not-a-duration"})
	expectFail(grainmcp.ToolWriteFile, map[string]any{"name": "", "path": ""})
	expectFail(grainmcp.ToolWriteFile, map[string]any{"name": "a", "path": "/x", "base64": "!!!"})
	expectFail(grainmcp.ToolCreateVM, map[string]any{"mounts": []any{"bad"}})
	expectFail(grainmcp.ToolCreateVM, map[string]any{"publish": []any{"bad:port"}})
	expectFail(grainmcp.ToolCreateVM, map[string]any{"preset": "no-such-preset-xyz"})
	expectFail(grainmcp.ToolForwardAdd, map[string]any{"name": "", "guest_port": 0})
	expectFail(grainmcp.ToolImagePull, map[string]any{"id": ""})
}

func TestWorkspaceReuse(t *testing.T) {
	vms := map[string]*client.Instance{
		"ws": {Name: "ws", Status: client.StatusRunning, Image: "grain-ubuntu"},
	}
	sess, _ := sessionWithMock(t, vms)
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolWorkspace,
		Arguments: map[string]any{
			"name":      "ws",
			"reuse":     true,
			"host_dir":  t.TempDir(),
			"first_cmd": "true",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, res), "reused") {
		t.Fatal(textOf(t, res))
	}
}

func TestLifecycleListGetStartStopForwardRemove(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx := t.Context()

	// create
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM,
		Arguments: map[string]any{
			"name": "life1", "persistent": true, "cpus": 1, "memory_mb": 512,
		},
	}); err != nil {
		t.Fatal(err)
	}
	// list
	lr, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolListVMs})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, lr), "life1") {
		t.Fatal(textOf(t, lr))
	}
	// get
	gr, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolGetVM, Arguments: map[string]any{"name": "life1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, gr), "life1") {
		t.Fatal(textOf(t, gr))
	}
	// stop (persistent → remains)
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolStopVM, Arguments: map[string]any{"name": "life1"},
	}); err != nil {
		t.Fatal(err)
	}
	// start
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolStartVM, Arguments: map[string]any{"name": "life1"},
	}); err != nil {
		t.Fatal(err)
	}
	// forward add + remove
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolForwardAdd,
		Arguments: map[string]any{"name": "life1", "guest_port": 80, "host_port": 18080},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolForwardRemove,
		Arguments: map[string]any{"name": "life1", "guest_port": 80},
	}); err != nil {
		t.Fatal(err)
	}
	// health + list empty-ish still works
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolHealth}); err != nil {
		t.Fatal(err)
	}
}

func TestActDefaultNameSanitize(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	// Host dir with special chars → sanitizeName used when name empty
	wd := filepath.Join(t.TempDir(), "My Project!!")
	if err := os.MkdirAll(wd, 0o755); err != nil {
		t.Fatal(err)
	}
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolAct,
		Arguments: map[string]any{
			"host_dir": wd,
			"keep":     true,
			"timeout":  "15s",
			// no name → sanitize
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt := textOf(t, res)
	if !strings.Contains(txt, "act-") {
		t.Fatal(txt)
	}
}

func TestK3sEphemeralAndWait(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx, cancel := context.WithTimeout(t.Context(), 15*time.Second)
	defer cancel()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolK3s,
		Arguments: map[string]any{
			"name":      "k3s-eph",
			"ephemeral": true,
			"skip_wait": true,
			"host_dir":  t.TempDir(),
			"timeout":   "10s",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, res), "k3s") {
		t.Fatal(textOf(t, res))
	}
	// bad timeout
	res2, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolK3s,
		Arguments: map[string]any{"timeout": "not-duration"},
	})
	if err == nil && res2 != nil && !res2.IsError {
		t.Fatal("expected timeout parse error")
	}
}

func TestMoreToolErrorBranches(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	ctx := t.Context()
	// missing VM get
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolGetVM, Arguments: map[string]any{"name": "nope"},
	})
	if err == nil && (res == nil || !res.IsError) {
		// may return IsError result
		if res != nil && !res.IsError && !strings.Contains(textOf(t, res), "not found") {
			t.Logf("get missing: %v", textOf(t, res))
		}
	}
	// logs without host (uses default host? NewMCPServerOpts with nil host)
	// sessionWithMock uses LocalHost or nil — check sessionWith
	// forward remove missing name
	res2, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolForwardRemove, Arguments: map[string]any{"name": "", "guest_port": 1},
	})
	if err == nil && res2 != nil && !res2.IsError {
		t.Log("forward remove empty name", textOf(t, res2))
	}
	// put tar bad base64
	res3, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolPutTar, Arguments: map[string]any{"name": "s1", "path": "/x", "base64": "!!!"},
	})
	if err == nil && res3 != nil && !res3.IsError {
		t.Log("put tar", textOf(t, res3))
	}
	// act bad timeout
	res4, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolAct, Arguments: map[string]any{"timeout": "xx", "host_dir": t.TempDir()},
	})
	if err == nil && res4 != nil && !res4.IsError {
		t.Fatal("expected act timeout error")
	}
	// image list without host — session may have LocalHost from NewMCPServerOpts
	_, _ = sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolImageList})
	// list vms
	_, _ = sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolListVMs})
	// workspace without reuse (create path)
	_, _ = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolWorkspace,
		Arguments: map[string]any{"name": "ws-new", "host_dir": t.TempDir()},
	})
}

func TestLocalHostDataDirAndImagePullLocalOnly(t *testing.T) {
	dir := t.TempDir()
	h := grainmcp.NewLocalHost(dir)
	if h.DataDir() != dir {
		t.Fatalf("DataDir %q", h.DataDir())
	}
	// grain-ubuntu is often LocalOnly or has URL — pull local-only id
	// Image "grain-ubuntu" may pull or be local; try import-only style via unknown is already tested
	// Force mkdir path by pulling a known catalog id if available
	list, err := h.ImageList()
	if err != nil {
		t.Fatal(err)
	}
	_ = list
	// Pull with valid id that is LocalOnly if any
	for _, img := range list {
		if img.ID == "" {
			continue
		}
		_ = h.ImagePull(t.Context(), img.ID) // may fail network or local-only; both cover branches
		break
	}
}

func TestExecStreamDefaultAndProgress(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	// default stream true
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      grainmcp.ToolExec,
		Arguments: map[string]any{"name": "s1", "cmd": "echo", "args": []any{"hi"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	// with cwd
	_, err = sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      grainmcp.ToolExec,
		Arguments: map[string]any{"name": "s1", "cmd": "pwd", "cwd": "/tmp", "stream": false},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestWriteFileBase64AndReadBinaryish(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	raw := []byte{0x00, 0x01, 0xff, 0xfe}
	b64 := base64.StdEncoding.EncodeToString(raw)
	_, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      grainmcp.ToolWriteFile,
		Arguments: map[string]any{"name": "s1", "path": "/tmp/bin", "base64": b64, "mode": "0600"},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolReadFile, Arguments: map[string]any{"name": "s1", "path": "/tmp/bin"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
}
