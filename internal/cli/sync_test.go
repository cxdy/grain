package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vmsync"
)

func TestCmdSyncRegistered(t *testing.T) {
	root := Root("test")
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "sync" {
			found = true
			var push, pull bool
			for _, sub := range c.Commands() {
				if sub.Name() == "push" {
					push = true
				}
				if sub.Name() == "pull" {
					pull = true
				}
			}
			if !push || !pull {
				t.Fatalf("sync missing push/pull: push=%v pull=%v", push, pull)
			}
			break
		}
	}
	if !found {
		t.Fatal("sync command not registered")
	}
}

func TestSyncHelpMentionsPushPull(t *testing.T) {
	root := Root("test")
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetArgs([]string{"sync", "--help"})
	if err := root.Execute(); err != nil {
		t.Fatal(err)
	}
	s := buf.String()
	if !strings.Contains(s, "push") || !strings.Contains(s, "pull") {
		t.Fatalf("help missing push/pull: %s", s)
	}
}

func TestSyncPushUseLine(t *testing.T) {
	cfg := ""
	cmd := cmdSyncPush(&cfg)
	if !strings.Contains(cmd.Use, "HOST_DIR") || !strings.Contains(cmd.Use, "GUEST_DIR") {
		t.Fatalf("use: %s", cmd.Use)
	}
	if !strings.Contains(cmd.Example, "sync push") {
		t.Fatalf("example: %s", cmd.Example)
	}
}

func TestParseArgsViaCPSpec(t *testing.T) {
	host, vm, guest, err := vmsync.ParseArgs(vmsync.Push, "/tmp/proj", "box:/work/proj", func(s string) (bool, string, string) {
		spec := parseCPSpec(s)
		return spec.Guest, spec.Name, spec.Path
	})
	if err != nil {
		t.Fatal(err)
	}
	if host != "/tmp/proj" || vm != "box" || guest != "/work/proj" {
		t.Fatalf("got %q %q %q", host, vm, guest)
	}
}

func TestSyncAPIIdentity(t *testing.T) {
	cfg := config.Config{DataDir: t.TempDir(), Socket: "/tmp/grain-test.sock"}
	id := syncAPIIdentity(cfg)
	if id != "local:/tmp/grain-test.sock" {
		t.Fatalf("got %q", id)
	}
	// With remote API URL
	apiURLFlag = "http://host:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	id = syncAPIIdentity(cfg)
	if id != "http://host:7474" {
		t.Fatalf("remote id %q", id)
	}
}

func TestExitWithHelpers(t *testing.T) {
	t.Parallel()
	// with(nil) returns the bare exitCodeError
	e := exitCodeError(2).with(nil)
	ec, ok := e.(exitCodeError)
	if !ok {
		t.Fatalf("%T", e)
	}
	if ec.Error() != "exit status 2" || ec.ExitCode() != 2 {
		t.Fatalf("%q %d", ec.Error(), ec.ExitCode())
	}
	wrapped := exitCodeError(3).with(errors.New("boom"))
	ew, ok := wrapped.(*exitWith)
	if !ok {
		t.Fatalf("%T", wrapped)
	}
	if ew.Error() != "boom" || ew.ExitCode() != 3 {
		t.Fatalf("%+v", ew)
	}
	if ew.Unwrap() == nil || ew.Unwrap().Error() != "boom" {
		t.Fatal("unwrap")
	}
}

func TestSyncProgressHook(t *testing.T) {
	// nil prog → nil hook
	if syncProgressHook(nil) != nil {
		t.Fatal("expected nil")
	}
	p := newTransferProgress("sync")
	t.Cleanup(func() { p.Finish("") })
	h := syncProgressHook(p)
	if h == nil {
		t.Fatal("expected hook")
	}
	h(vmsync.ProgressEvent{Phase: "plan", Index: 0, Total: 0})
	h(vmsync.ProgressEvent{Phase: "put", Action: "create", RelPath: "a.txt", Index: 1, Total: 3})
	h(vmsync.ProgressEvent{Phase: "done", Action: "done", RelPath: ""})
}

// syncMockDaemon serves agent FS routes needed for dry-run sync via NewAPIFS.
func syncMockDaemon(t *testing.T, guestRootType string, entries []agent.FSInfo) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
	})
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		// root dir
		if p == "/work" || p == "/work/" {
			_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: "work", Type: guestRootType, Size: 0, Mode: "0755"})
			return
		}
		// missing nested
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		if entries == nil {
			_ = json.NewEncoder(w).Encode([]agent.FSInfo{})
			return
		}
		_ = json.NewEncoder(w).Encode(entries)
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("x"))
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestRunSyncCmdDryRunPush(t *testing.T) {
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := syncMockDaemon(t, "directory", nil)
	cfgPath := withRemoteCfg(t, srv.URL)

	err := runSyncCmd(&cfgPath, vmsync.Push, hostDir, "lab:/work", syncFlags{dryRun: true, verbose: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunSyncCmdDryRunPull(t *testing.T) {
	hostDir := t.TempDir() // dest must exist for host inventory (dry-run)
	srv := syncMockDaemon(t, "directory", []agent.FSInfo{
		{Name: "remote.txt", Type: "file", Size: 2, Mode: "0644", Mtime: 1},
	})
	cfgPath := withRemoteCfg(t, srv.URL)

	err := runSyncCmd(&cfgPath, vmsync.Pull, "lab:/work", hostDir, syncFlags{dryRun: true})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunSyncCmdPushApply(t *testing.T) {
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "a.txt"), []byte("abc"), 0o644); err != nil {
		t.Fatal(err)
	}
	// Stateful mock: files put via PUT are returned by Stat/ReadDir.
	files := map[string]string{}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		if p == "/work" || p == "/work/" {
			_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: "work", Type: "directory", Mode: "0755"})
			return
		}
		base := filepath.Base(p)
		if _, ok := files[base]; ok {
			_ = json.NewEncoder(w).Encode(agent.FSInfo{Name: base, Type: "file", Size: int64(len(files[base])), Mode: "0644", Mtime: 1})
			return
		}
		http.Error(w, `{"error":"not found"}`, 404)
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		var ents []agent.FSInfo
		for name, body := range files {
			ents = append(ents, agent.FSInfo{Name: name, Type: "file", Size: int64(len(body)), Mode: "0644", Mtime: 1})
		}
		if ents == nil {
			ents = []agent.FSInfo{}
		}
		_ = json.NewEncoder(w).Encode(ents)
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("path")
		body, _ := io.ReadAll(r.Body)
		files[filepath.Base(p)] = string(body)
		w.WriteHeader(http.StatusNoContent)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	cfgPath := withRemoteCfg(t, srv.URL)

	err := runSyncCmd(&cfgPath, vmsync.Push, hostDir, "lab:/work", syncFlags{})
	if err != nil {
		t.Fatal(err)
	}
}

func TestRunSyncCmdUsageErrors(t *testing.T) {
	cfgPath := withRemoteCfg(t, "http://127.0.0.1:1")

	// relative guest path
	err := runSyncCmd(&cfgPath, vmsync.Push, t.TempDir(), "lab:relative", syncFlags{})
	if err == nil {
		t.Fatal("expected relative guest path error")
	}
	// bad parse: host as guest for push
	err = runSyncCmd(&cfgPath, vmsync.Push, "lab:/work", t.TempDir(), syncFlags{})
	if err == nil {
		t.Fatal("expected parse error for swapped args")
	}
}

func TestRunSyncCmdLoadCfgError(t *testing.T) {
	bad := t.TempDir() // directory, not a file
	err := runSyncCmd(&bad, vmsync.Push, t.TempDir(), "lab:/work", syncFlags{dryRun: true})
	if err == nil {
		t.Fatal("expected load error")
	}
}

func TestRunSyncCmdClientAuthError(t *testing.T) {
	// Non-loopback remote without token → requireRemoteAuth fails
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	apiURLFlag = "http://10.9.9.9:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	err := runSyncCmd(&cfgPath, vmsync.Push, dir, "lab:/work", syncFlags{dryRun: true})
	if err == nil {
		t.Fatal("expected auth error")
	}
}

func TestOpenSyncFSRemote(t *testing.T) {
	srv := syncMockDaemon(t, "directory", nil)
	cfg := config.Config{DataDir: t.TempDir()}
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	c, err := clientFrom(cfg)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := openSyncFS(context.Background(), cfg, c, "lab")
	if err != nil {
		t.Fatal(err)
	}
	st, err := fs.Stat(context.Background(), "/work")
	if err != nil || st == nil || st.Type != "directory" {
		t.Fatalf("%+v %v", st, err)
	}
}

func TestOpenSyncFSLocalAgentMissing(t *testing.T) {
	// Local mode (no API URL) with dead/missing agent dial.
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfg := config.Config{DataDir: dir, Socket: filepath.Join(dir, "missing.sock")}
	// Client that returns a VM without a working agent port.
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vms/{name}", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "lab", "status": "running", "ip": "127.0.0.1", "agent_port": 1, "ssh_port": 1,
		})
	})
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) })
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	// Force local mode by clearing remote URL even though client uses HTTP —
	// openSyncFS checks remoteMode(cfg), not the client base.
	_, err := openSyncFS(context.Background(), cfg, c, "lab")
	if err == nil {
		t.Fatal("expected agent unavailable")
	}
	if !strings.Contains(err.Error(), "agent") {
		t.Fatalf("%v", err)
	}
}

func TestCmdSyncPushPullExecute(t *testing.T) {
	hostDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(hostDir, "f"), []byte("1"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := syncMockDaemon(t, "directory", nil)
	cfgPath := withRemoteCfg(t, srv.URL)

	push := cmdSyncPush(&cfgPath)
	push.SetArgs([]string{"--dry-run", hostDir, "lab:/work"})
	if err := push.Execute(); err != nil {
		t.Fatalf("push: %v", err)
	}

	pull := cmdSyncPull(&cfgPath)
	out := t.TempDir()
	pull.SetArgs([]string{"--dry-run", "lab:/work", out})
	if err := pull.Execute(); err != nil {
		t.Fatalf("pull: %v", err)
	}
}

func TestRunSyncCmdGuestNotDir(t *testing.T) {
	hostDir := t.TempDir()
	srv := syncMockDaemon(t, "file", nil) // guest root is a file → push fails validate
	cfgPath := withRemoteCfg(t, srv.URL)
	err := runSyncCmd(&cfgPath, vmsync.Push, hostDir, "lab:/work", syncFlags{dryRun: true})
	if err == nil {
		t.Fatal("expected not-a-directory error")
	}
}

func TestBindSyncFlagsPresent(t *testing.T) {
	cfg := ""
	cmd := cmdSyncPush(&cfg)
	for _, name := range []string{"delete", "dry-run", "force", "checksum", "exclude", "no-defaults", "verbose", "max-file-size"} {
		if cmd.Flags().Lookup(name) == nil {
			t.Fatalf("missing flag %s", name)
		}
	}
	if cmd.Flags().Lookup("checksum").Hidden {
		t.Fatal("checksum flag should be visible")
	}
}

func TestSyncChecksumShownInHelp(t *testing.T) {
	cfg := ""
	cmd := cmdSyncPush(&cfg)
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "--checksum") {
		t.Fatalf("help should list --checksum: %s", buf.String())
	}
}
