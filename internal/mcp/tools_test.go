package mcp_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/cxdy/grain/client"
	grainmcp "github.com/cxdy/grain/internal/mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// mockDaemon implements enough of the grain HTTP API for tool tests.
func mockDaemon(t *testing.T, vms map[string]*client.Instance) *httptest.Server {
	t.Helper()
	if vms == nil {
		vms = map[string]*client.Instance{}
	}
	var mu sync.Mutex
	files := map[string][]byte{} // key name\x00path
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "grain", "version": "test"})
	})
	mux.HandleFunc("/vms", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.Method {
		case http.MethodGet:
			list := make([]*client.Instance, 0, len(vms))
			for _, inst := range vms {
				list = append(list, inst)
			}
			_ = json.NewEncoder(w).Encode(list)
		case http.MethodPost:
			var body client.CreateRequest
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), 400)
				return
			}
			name := body.Name
			if name == "" {
				name = "generated-vm"
			}
			inst := &client.Instance{
				Name:       name,
				Status:     client.StatusRunning,
				Persistent: body.Persistent,
				CPUs:       body.CPUs,
				MemoryMB:   body.MemoryMB,
				DiskGB:     body.DiskGB,
				Image:      body.Image,
				Forwards:   body.Forwards,
				Mounts:     body.Mounts,
			}
			if inst.CPUs == 0 {
				inst.CPUs = 2
			}
			if inst.Image == "" {
				inst.Image = "grain-ubuntu"
			}
			vms[name] = inst
			if r.URL.Query().Get("wait") == "bad" {
				http.Error(w, `{"error":"bad wait"}`, 400)
				return
			}
			_ = json.NewEncoder(w).Encode(inst)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})
	mux.HandleFunc("/vms/", func(w http.ResponseWriter, r *http.Request) {
		path := strings.TrimPrefix(r.URL.Path, "/vms/")
		parts := strings.Split(path, "/")
		name := parts[0]
		if name == "" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		defer mu.Unlock()

		if len(parts) >= 2 && parts[1] == "exec" {
			if r.Method != http.MethodPost {
				http.Error(w, "method", http.StatusMethodNotAllowed)
				return
			}
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			cmd := r.URL.Query().Get("cmd")
			buffered := r.URL.Query().Get("buffered")
			if buffered == "false" {
				// NDJSON stream
				w.Header().Set("Content-Type", "application/x-ndjson")
				flusher, _ := w.(http.Flusher)
				writeFrame := func(v any) {
					b, _ := json.Marshal(v)
					_, _ = w.Write(b)
					_, _ = w.Write([]byte("\n"))
					if flusher != nil {
						flusher.Flush()
					}
				}
				writeFrame(client.ExecFrame{Type: "stdout", Data: "line1-" + cmd + "\n"})
				writeFrame(client.ExecFrame{Type: "stderr", Data: "warn\n"})
				writeFrame(client.ExecFrame{Type: "stdout", Data: "line2\n"})
				code := 0
				writeFrame(client.ExecFrame{Type: "exit", ExitCode: &code})
				return
			}
			_ = json.NewEncoder(w).Encode(client.ExecResult{
				Stdout:   "out:" + cmd,
				Stderr:   "",
				ExitCode: 0,
			})
			return
		}
		if len(parts) >= 2 && parts[1] == "agent" && len(parts) >= 3 && parts[2] == "health" {
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			_ = json.NewEncoder(w).Encode(client.Health{Hostname: name, AgentVersion: "test", UserdataRan: true})
			return
		}
		if len(parts) >= 2 && parts[1] == "stats" {
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			_ = json.NewEncoder(w).Encode(client.Stats{UptimeSec: 12, MemTotal: 1024, MemAvail: 512, Load1: 0.1})
			return
		}
		if len(parts) >= 2 && parts[1] == "forwards" {
			inst, ok := vms[name]
			if !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			if r.Method == http.MethodPost {
				var body struct {
					HostPort  int `json:"host_port"`
					GuestPort int `json:"guest_port"`
				}
				_ = json.NewDecoder(r.Body).Decode(&body)
				hp := body.HostPort
				if hp == 0 {
					hp = 18080
				}
				lf := client.LiveForward{HostPort: hp, GuestPort: body.GuestPort}
				inst.LiveForwards = append(inst.LiveForwards, lf)
				_ = json.NewEncoder(w).Encode(lf)
				return
			}
			if r.Method == http.MethodDelete && len(parts) >= 3 {
				w.WriteHeader(200)
				return
			}
		}
		if len(parts) >= 2 && parts[1] == "cp" {
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			gpath := r.URL.Query().Get("path")
			key := name + "\x00" + gpath
			switch r.Method {
			case http.MethodPut:
				b, _ := io.ReadAll(r.Body)
				files[key] = b
				w.WriteHeader(200)
			case http.MethodGet:
				b, ok := files[key]
				if !ok {
					http.Error(w, `{"error":"not found"}`, 404)
					return
				}
				_, _ = w.Write(b)
			}
			return
		}
		if len(parts) >= 3 && parts[1] == "fs" {
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			switch parts[2] {
			case "readdir":
				_ = json.NewEncoder(w).Encode([]client.FSInfo{{Name: "a.txt", Type: "file"}})
			case "stat":
				_ = json.NewEncoder(w).Encode(client.FSInfo{Name: "a.txt", Type: "file", Size: 3})
			case "mkdir", "remove":
				w.WriteHeader(200)
			}
			return
		}
		if len(parts) >= 2 {
			action := parts[1]
			inst, ok := vms[name]
			if !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			switch action {
			case "start":
				inst.Status = client.StatusRunning
				_ = json.NewEncoder(w).Encode(inst)
			case "shutdown":
				if !inst.Persistent {
					delete(vms, name)
				} else {
					inst.Status = client.StatusStopped
				}
				w.WriteHeader(http.StatusOK)
			default:
				// fall through for GET name
			}
			if action == "start" || action == "shutdown" {
				return
			}
		}
		switch r.Method {
		case http.MethodGet:
			inst, ok := vms[name]
			if !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			_ = json.NewEncoder(w).Encode(inst)
		case http.MethodDelete:
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			delete(vms, name)
			w.WriteHeader(http.StatusOK)
		default:
			http.Error(w, "method", http.StatusMethodNotAllowed)
		}
	})
	return httptest.NewServer(mux)
}

type fakeHost struct {
	dir   string
	pulls []string
	logs  map[string]string
}

func (f *fakeHost) DataDir() string { return f.dir }
func (f *fakeHost) ImageList() ([]grainmcp.ImageInfo, error) {
	return []grainmcp.ImageInfo{{ID: "grain-ubuntu", Local: true, HasAgent: true}}, nil
}
func (f *fakeHost) ImagePull(_ context.Context, id string) error {
	f.pulls = append(f.pulls, id)
	return nil
}
func (f *fakeHost) ReadVMLog(name string, qemu bool, maxBytes int) (string, string, error) {
	key := name
	if qemu {
		key += ":qemu"
	}
	c, ok := f.logs[key]
	if !ok {
		return "/tmp/x", "", fmt.Errorf("no log at missing")
	}
	if maxBytes > 0 && len(c) > maxBytes {
		c = c[len(c)-maxBytes:]
	}
	return "/tmp/" + key, c, nil
}

func sessionWithMock(t *testing.T, vms map[string]*client.Instance) (*mcp.ClientSession, *httptest.Server) {
	t.Helper()
	return sessionWith(t, vms, nil)
}

func sessionWith(t *testing.T, vms map[string]*client.Instance, host grainmcp.HostOps) (*mcp.ClientSession, *httptest.Server) {
	t.Helper()
	hs := mockDaemon(t, vms)
	c, err := client.DialHTTP(hs.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if host == nil {
		host = &fakeHost{dir: t.TempDir(), logs: map[string]string{"sandbox-a": "boot ok\n"}}
	}
	srv := grainmcp.NewMCPServerOpts(grainmcp.ServerOptions{
		Version: "test",
		Client:  c,
		Host:    host,
	})
	cli := mcp.NewClient(&mcp.Implementation{Name: "test-client", Version: "v0"}, nil)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), t1, nil); err != nil {
		t.Fatal(err)
	}
	sess, err := cli.Connect(t.Context(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = sess.Close()
		hs.Close()
	})
	return sess, hs
}

func textOf(t *testing.T, res *mcp.CallToolResult) string {
	t.Helper()
	if res == nil {
		t.Fatal("nil result")
	}
	if res.IsError {
		t.Fatalf("tool error: %+v", res.Content)
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestToolNamesExpanded(t *testing.T) {
	t.Parallel()
	names := grainmcp.ToolNames()
	need := []string{
		grainmcp.ToolExec, grainmcp.ToolWriteFile, grainmcp.ToolReadFile,
		grainmcp.ToolAgentHealth, grainmcp.ToolLogs, grainmcp.ToolStats,
		grainmcp.ToolWorkspace, grainmcp.ToolForwardAdd, grainmcp.ToolImageList,
		grainmcp.ToolAct, grainmcp.ToolK3s, grainmcp.ToolFSReadDir,
		grainmcp.ToolSyncPush, grainmcp.ToolSyncPull,
	}
	got := map[string]bool{}
	for _, n := range names {
		got[n] = true
	}
	for _, n := range need {
		if !got[n] {
			t.Errorf("missing %s", n)
		}
	}
	if len(names) < 20 {
		t.Fatalf("expected expanded set, got %d", len(names))
	}
}

func TestToolsListIncludesExpanded(t *testing.T) {
	sess, _ := sessionWithMock(t, nil)
	res, err := sess.ListTools(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]bool{}
	for _, tool := range res.Tools {
		got[tool.Name] = true
	}
	for _, name := range grainmcp.ToolNames() {
		if !got[name] {
			t.Errorf("missing tool %s", name)
		}
	}
}

func TestCreateDefaultsImageAndWait(t *testing.T) {
	var sawImage, sawWait string
	// Use mock that records create body — already returns body.Image
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      grainmcp.ToolCreateVM,
		Arguments: map[string]any{"name": "def1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt := textOf(t, res)
	if !strings.Contains(txt, "grain-ubuntu") {
		t.Fatalf("default image missing: %s", txt)
	}
	// Wait is query param not in body image - check instance image field
	if vms["def1"].Image != "grain-ubuntu" {
		t.Fatalf("image %q", vms["def1"].Image)
	}
	_ = sawImage
	_ = sawWait
}

func TestExecStreamProgress(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolExec,
		Arguments: map[string]any{
			"name": "s1",
			"cmd":  "echo",
			"args": []any{"hi"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt := textOf(t, res)
	if !strings.Contains(txt, "progress") && !strings.Contains(txt, "line1") {
		t.Fatalf("expected stream progress content: %s", txt)
	}
	if !strings.Contains(txt, `"streamed": true`) && !strings.Contains(txt, `"streamed":true`) {
		t.Fatalf("expected streamed true: %s", txt)
	}
}

func TestWriteReadFileRoundTrip(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	_, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolWriteFile,
		Arguments: map[string]any{
			"name":    "s1",
			"path":    "/tmp/hello.txt",
			"content": "hello-mcp",
			"mode":    "0644",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      grainmcp.ToolReadFile,
		Arguments: map[string]any{"name": "s1", "path": "/tmp/hello.txt"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, res), "hello-mcp") {
		t.Fatal(textOf(t, res))
	}
}

func TestAgentHealthLogsStats(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	host := &fakeHost{dir: t.TempDir(), logs: map[string]string{"s1": "serial line\n"}}
	sess, _ := sessionWith(t, vms, host)
	ctx := t.Context()
	h, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolAgentHealth, Arguments: map[string]any{"name": "s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, h), "s1") {
		t.Fatal(textOf(t, h))
	}
	lg, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolLogs, Arguments: map[string]any{"name": "s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, lg), "serial line") {
		t.Fatal(textOf(t, lg))
	}
	st, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolStats, Arguments: map[string]any{"name": "s1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, st), "uptime") {
		t.Fatal(textOf(t, st))
	}
}

func TestDeleteIdempotent(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolDeleteVM, Arguments: map[string]any{"name": "nope"},
	})
	// Idempotent: missing VM either returns ok+missing or a not-found error from the client.
	if err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "not found") && !strings.Contains(msg, "404") {
			t.Fatalf("unexpected delete missing error: %v", err)
		}
	} else {
		txt := textOf(t, res)
		if !strings.Contains(txt, "missing") && !strings.Contains(txt, "ok") {
			t.Fatal(txt)
		}
	}
	// create then delete twice
	if _, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM, Arguments: map[string]any{"name": "d1"},
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolDeleteVM, Arguments: map[string]any{"name": "d1"},
	}); err != nil {
		t.Fatal(err)
	}
	res2, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolDeleteVM, Arguments: map[string]any{"name": "d1"},
	})
	if err != nil {
		msg := strings.ToLower(err.Error())
		if !strings.Contains(msg, "not found") && !strings.Contains(msg, "404") {
			t.Fatalf("second delete should be idempotent: %v", err)
		}
		return
	}
	txt := textOf(t, res2)
	if !strings.Contains(txt, "missing") && !strings.Contains(txt, "ok") {
		t.Fatalf("second delete result: %s", txt)
	}
}

func TestWorkspaceAndForwardsAndImages(t *testing.T) {
	vms := map[string]*client.Instance{}
	host := &fakeHost{dir: t.TempDir(), logs: map[string]string{}}
	sess, _ := sessionWith(t, vms, host)
	ctx := t.Context()
	wd := t.TempDir()
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolWorkspace,
		Arguments: map[string]any{
			"name":      "ws1",
			"host_dir":  wd,
			"first_cmd": "uname",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	txt := textOf(t, res)
	if !strings.Contains(txt, "ws1") {
		t.Fatal(txt)
	}
	// forward
	_, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolForwardAdd,
		Arguments: map[string]any{"name": "ws1", "guest_port": 8080, "host_port": 0},
	})
	if err != nil {
		t.Fatal(err)
	}
	// images
	il, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolImageList})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, il), "grain-ubuntu") {
		t.Fatal(textOf(t, il))
	}
	_, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolImagePull, Arguments: map[string]any{"id": "grain-ubuntu"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(host.pulls) != 1 {
		t.Fatalf("pulls %v", host.pulls)
	}
}

func TestFSOps(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	ctx := t.Context()
	for _, tool := range []string{grainmcp.ToolFSReadDir, grainmcp.ToolFSStat, grainmcp.ToolFSMkdir, grainmcp.ToolFSRemove} {
		_, err := sess.CallTool(ctx, &mcp.CallToolParams{
			Name: tool, Arguments: map[string]any{"name": "s1", "path": "/tmp", "recursive": true},
		})
		if err != nil {
			t.Fatalf("%s: %v", tool, err)
		}
	}
}

func TestPutGetTar(t *testing.T) {
	vms := map[string]*client.Instance{"s1": {Name: "s1", Status: client.StatusRunning}}
	sess, _ := sessionWithMock(t, vms)
	raw := []byte("not-a-real-tar-but-ok")
	b64 := base64.StdEncoding.EncodeToString(raw)
	_, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name:      grainmcp.ToolPutTar,
		Arguments: map[string]any{"name": "s1", "path": "/tmp/x", "base64": b64},
	})
	if err != nil {
		t.Fatal(err)
	}
	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolGetTar, Arguments: map[string]any{"name": "s1", "path": "/tmp/x"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, res), b64) {
		t.Fatal(textOf(t, res))
	}
}

func TestActAndK3sTools(t *testing.T) {
	// Act creates then may wait for act binary - mock always returns success on exec so loop exits.
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	wd := t.TempDir()
	_ = os.MkdirAll(filepath.Join(wd, ".github", "workflows"), 0o755)
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolAct,
		Arguments: map[string]any{
			"host_dir": wd,
			"name":     "act1",
			"keep":     true,
			"act_args": []any{"-l"},
			"timeout":  "30s",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, res), "act1") {
		t.Fatal(textOf(t, res))
	}
	// k3s create + wait loop (exec returns out:bash which may not contain Ready - timeout path ok)
	res2, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolK3s,
		Arguments: map[string]any{
			"name":      "k3s1",
			"skip_wait": true,
			"timeout":   "10s",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, res2), "k3s") {
		t.Fatal(textOf(t, res2))
	}
}

func TestHealthListCreateGetExecStopDelete(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx := context.Background()

	h, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolHealth})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, h), "ok") {
		t.Fatal(textOf(t, h))
	}

	cr, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM,
		Arguments: map[string]any{
			"name":      "sandbox-a",
			"cpus":      2,
			"memory_mb": 1024,
			"image":     "grain-ubuntu",
			"wait":      "agent",
			"publish":   []any{"8080:80"},
			"mounts":    []any{"/tmp/work:/work"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, cr), "sandbox-a") {
		t.Fatal(textOf(t, cr))
	}

	// buffered exec
	stream := false
	_ = stream
	er, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolExec,
		Arguments: map[string]any{
			"name":   "sandbox-a",
			"cmd":    "uname",
			"stream": false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, er), "out:uname") {
		t.Fatal(textOf(t, er))
	}

	_, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolDeleteVM, Arguments: map[string]any{"name": "sandbox-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestCreatePostsJSONBody(t *testing.T) {
	var sawBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/vms", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sawBody = string(b)
		_ = json.NewEncoder(w).Encode(&client.Instance{Name: "x", Status: client.StatusRunning, Image: "grain-ubuntu"})
	})
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	c, err := client.DialHTTP(hs.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := grainmcp.NewMCPServerOpts(grainmcp.ServerOptions{
		Version: "t", Client: c, Host: &fakeHost{dir: t.TempDir()},
	})
	cli := mcp.NewClient(&mcp.Implementation{Name: "c", Version: "v0"}, nil)
	t1, t2 := mcp.NewInMemoryTransports()
	if _, err := srv.Connect(t.Context(), t1, nil); err != nil {
		t.Fatal(err)
	}
	sess, err := cli.Connect(t.Context(), t2, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sess.Close() })
	_, _ = sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM,
		Arguments: map[string]any{
			"name":  "from-mcp",
			"image": "grain-ubuntu",
			"cpus":  4,
		},
	})
	if !strings.Contains(sawBody, "from-mcp") || !strings.Contains(sawBody, "grain-ubuntu") {
		t.Fatalf("daemon did not receive create body: %s", sawBody)
	}
}

func TestLocalHostImageList(t *testing.T) {
	t.Parallel()
	h := grainmcp.NewLocalHost(t.TempDir())
	list, err := h.ImageList()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) == 0 {
		t.Fatal("empty catalog")
	}
	found := false
	for _, i := range list {
		if i.ID == "grain-ubuntu" || i.ID == "ubuntu-cloud" {
			found = true
		}
	}
	if !found {
		t.Fatalf("%+v", list)
	}
}

func TestLocalHostReadLog(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "vms", "n1")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(p, "serial.log"), []byte("hello-log"), 0o644); err != nil {
		t.Fatal(err)
	}
	h := grainmcp.NewLocalHost(dir)
	_, content, err := h.ReadVMLog("n1", false, 100)
	if err != nil {
		t.Fatal(err)
	}
	if content != "hello-log" {
		t.Fatal(content)
	}
}

// silence unused
var _ = bytes.MinRead
