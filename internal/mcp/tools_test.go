package mcp_test

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

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
			vms[name] = inst
			// Surface wait query for assertions.
			if r.URL.Query().Get("wait") == "bad" {
				http.Error(w, `{"error":"bad wait"}`, 400)
				return
			}
			_ = json.NewEncoder(w).Encode(inst)
		default:
			http.Error(w, "method", 405)
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
		// /vms/{name}/exec
		if len(parts) >= 2 && parts[1] == "exec" {
			if r.Method != http.MethodPost {
				http.Error(w, "method", 405)
				return
			}
			if _, ok := vms[name]; !ok {
				http.Error(w, `{"error":"not found"}`, 404)
				return
			}
			cmd := r.URL.Query().Get("cmd")
			_ = json.NewEncoder(w).Encode(client.ExecResult{
				Stdout:   "out:" + cmd,
				Stderr:   "",
				ExitCode: 0,
			})
			return
		}
		// /vms/{name}/start | shutdown
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
				http.Error(w, "unknown action", 404)
			}
			return
		}
		// GET or DELETE /vms/{name}
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
			http.Error(w, "method", 405)
		}
	})
	return httptest.NewServer(mux)
}

func sessionWithMock(t *testing.T, vms map[string]*client.Instance) (*mcp.ClientSession, *httptest.Server) {
	t.Helper()
	hs := mockDaemon(t, vms)
	c, err := client.DialHTTP(hs.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := grainmcp.NewMCPServer("test", c)
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

func TestToolNamesStable(t *testing.T) {
	t.Parallel()
	names := grainmcp.ToolNames()
	want := []string{
		grainmcp.ToolHealth, grainmcp.ToolListVMs, grainmcp.ToolGetVM,
		grainmcp.ToolCreateVM, grainmcp.ToolStartVM, grainmcp.ToolStopVM,
		grainmcp.ToolDeleteVM, grainmcp.ToolExec,
	}
	if len(names) != len(want) {
		t.Fatalf("%v", names)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("%d: %s != %s", i, names[i], want[i])
		}
	}
}

func TestToolsListIncludesLifecycle(t *testing.T) {
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
			t.Errorf("missing tool %s in %v", name, got)
		}
	}
}

func TestHealthListCreateGetExecStopDelete(t *testing.T) {
	vms := map[string]*client.Instance{}
	sess, _ := sessionWithMock(t, vms)
	ctx := context.Background()

	// health
	h, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolHealth})
	if err != nil {
		t.Fatal(err)
	}
	ht := textOf(t, h)
	if !strings.Contains(ht, `"ok": true`) && !strings.Contains(ht, `"ok":true`) {
		t.Fatalf("health: %s", ht)
	}

	// create
	cr, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM,
		Arguments: map[string]any{
			"name":        "sandbox-a",
			"cpus":        2,
			"memory_mb":   1024,
			"image":       "grain-ubuntu",
			"wait":        "agent",
			"publish":     []any{"8080:80"},
			"mounts":      []any{"/tmp/work:/work"},
			"persistent":  false,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	ct := textOf(t, cr)
	if !strings.Contains(ct, "sandbox-a") {
		t.Fatalf("create: %s", ct)
	}

	// list
	lr, err := sess.CallTool(ctx, &mcp.CallToolParams{Name: grainmcp.ToolListVMs})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, lr), "sandbox-a") {
		t.Fatalf("list: %s", textOf(t, lr))
	}

	// get
	gr, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolGetVM,
		Arguments: map[string]any{"name": "sandbox-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, gr), "running") {
		t.Fatalf("get: %s", textOf(t, gr))
	}

	// exec
	er, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolExec,
		Arguments: map[string]any{
			"name": "sandbox-a",
			"cmd":  "uname",
			"args": []any{"-a"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, er), "out:uname") {
		t.Fatalf("exec: %s", textOf(t, er))
	}

	// stop (ephemeral → deleted in mock)
	sr, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolStopVM,
		Arguments: map[string]any{"name": "sandbox-a"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = textOf(t, sr)

	// recreate for delete path
	_, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolCreateVM,
		Arguments: map[string]any{"name": "sandbox-b", "persistent": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	// stop persistent keeps it
	if _, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolStopVM, Arguments: map[string]any{"name": "sandbox-b"},
	}); err != nil {
		t.Fatal(err)
	}
	// start
	st, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolStartVM, Arguments: map[string]any{"name": "sandbox-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(textOf(t, st), "running") {
		t.Fatalf("start: %s", textOf(t, st))
	}
	// delete
	dr, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolDeleteVM, Arguments: map[string]any{"name": "sandbox-b"},
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = textOf(t, dr)

	// get missing → error
	_, err = sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolGetVM, Arguments: map[string]any{"name": "nope"},
	})
	if err == nil {
		// SDK may return result with IsError instead of Go error
		// try call and inspect
	}
	bad, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name: grainmcp.ToolGetVM, Arguments: map[string]any{"name": "nope"},
	})
	if err == nil && bad != nil && !bad.IsError {
		// Some SDK versions wrap tool errors as IsError on result
		t.Fatalf("expected error for missing vm, got %v", bad)
	}
}

func TestCreateValidationAndAPIErrors(t *testing.T) {
	sess, _ := sessionWithMock(t, nil)
	ctx := context.Background()

	// missing name for get
	res, err := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolGetVM,
		Arguments: map[string]any{"name": ""},
	})
	if err == nil && (res == nil || !res.IsError) {
		// empty name returns toolErr — check either path
		if err == nil {
			// read content for error text if IsError
			if res != nil && res.IsError {
				return
			}
			// If SDK returns error as CallTool error:
		}
	}
	if err == nil && res != nil && !res.IsError {
		t.Fatalf("expected validation error, got %s", textSafe(res))
	}

	// exec missing cmd
	res2, err2 := sess.CallTool(ctx, &mcp.CallToolParams{
		Name:      grainmcp.ToolExec,
		Arguments: map[string]any{"name": "x", "cmd": ""},
	})
	if err2 == nil && res2 != nil && !res2.IsError {
		t.Fatalf("expected cmd required error")
	}
}

func textSafe(res *mcp.CallToolResult) string {
	if res == nil {
		return ""
	}
	var b strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			b.WriteString(tc.Text)
		}
	}
	return b.String()
}

func TestParsePublishViaCreate(t *testing.T) {
	// Ensure create with bad publish fails before HTTP.
	vms := map[string]*client.Instance{}
	// Use Server methods indirectly: bad mount
	hs := mockDaemon(t, vms)
	t.Cleanup(hs.Close)
	c, err := client.DialHTTP(hs.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := grainmcp.NewMCPServer("t", c)
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

	res, err := sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM,
		Arguments: map[string]any{
			"name":   "m",
			"mounts": []any{"nocolon"},
		},
	})
	if err == nil && res != nil && !res.IsError {
		t.Fatal("expected mount parse error")
	}
}

// Ensure mock does not swallow request bodies (client path integrity).
func TestCreatePostsJSONBody(t *testing.T) {
	var sawBody string
	mux := http.NewServeMux()
	mux.HandleFunc("/vms", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		sawBody = string(b)
		_ = json.NewEncoder(w).Encode(&client.Instance{Name: "x", Status: client.StatusRunning})
	})
	hs := httptest.NewServer(mux)
	t.Cleanup(hs.Close)
	c, err := client.DialHTTP(hs.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	srv := grainmcp.NewMCPServer("t", c)
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
	_, err = sess.CallTool(t.Context(), &mcp.CallToolParams{
		Name: grainmcp.ToolCreateVM,
		Arguments: map[string]any{
			"name":  "from-mcp",
			"image": "grain-ubuntu",
			"cpus":  4,
		},
	})
	if err != nil {
		// may still succeed with result
	}
	if !strings.Contains(sawBody, "from-mcp") || !strings.Contains(sawBody, "grain-ubuntu") {
		t.Fatalf("daemon did not receive create body: %s", sawBody)
	}
}
