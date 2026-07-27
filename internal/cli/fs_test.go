package cli

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/vm"
)

func TestResolveVMAndPathOneArg(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms" {
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "only-vm", Status: "running"}})
			return
		}
		http.NotFound(w, r)
	})
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	name, path, err := resolveVMAndPath(c, []string{"/tmp"})
	if err != nil {
		t.Fatal(err)
	}
	if name != "only-vm" || path != "/tmp" {
		t.Fatalf("%q %q", name, path)
	}
}

func TestResolveVMAndPathOneArgNoVMs(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{})
	})
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if _, _, err := resolveVMAndPath(c, []string{"/tmp"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdFsMkdirFlag(t *testing.T) {
	cfg := ""
	cmd := cmdFsMkdir(&cfg)
	if cmd.Flags().Lookup("parents") == nil {
		t.Fatal("missing --parents")
	}
	cmd = cmdFsRm(&cfg)
	if cmd.Flags().Lookup("recursive") == nil {
		t.Fatal("missing --recursive")
	}
}

func TestCmdFsNeedPathArgs(t *testing.T) {
	cfg := ""
	cmd := cmdFsLs(&cfg)
	// cobra RangeArgs(1,2) — zero args should fail at args validation
	cmd.SetArgs([]string{})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected args error")
	}
}

func TestCmdCpRemoteMode(t *testing.T) {
	// remote mode uses daemon-proxied copy (no scp fallback on failure)
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	cmd := cmdCp(&cfg)
	// missing host src → error
	cmd.SetArgs([]string{"/no/such/src-file-xyz", "vm:/tmp/y"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
	// force agent + ssh mutual exclusion already covered; --agent remote with missing file
	cmd = cmdCp(&cfg)
	cmd.SetArgs([]string{"--agent", "/no/such", "vm:/g"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdFsLocalAgentPaths(t *testing.T) {
	// Real guest agent on localhost + unix-socket daemon mock that points AgentPort at it.
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
		t.Fatal("no agent port")
	}

	dir := t.TempDir()
	sock := filepath.Join(dir, "grain.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	mux := http.NewServeMux()
	mux.HandleFunc("/vms", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "v", Status: "running", AgentPort: port, IP: "127.0.0.1"}})
	})
	mux.HandleFunc("/vms/v", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "v", Status: "running", AgentPort: port, IP: "127.0.0.1"})
	})
	hs := &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = hs.Serve(ln) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = hs.Shutdown(ctx)
	})

	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfgPath := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\nsocket: "+sock+"\nhypervisor: mock\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	gdir := t.TempDir()
	if err := os.WriteFile(filepath.Join(gdir, "hello.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	ls := cmdFsLs(&cfgPath)
	ls.SetArgs([]string{"v", gdir})
	if err := ls.Execute(); err != nil {
		t.Fatalf("fs ls local agent: %v", err)
	}
	st := cmdFsStat(&cfgPath)
	st.SetArgs([]string{"v", filepath.Join(gdir, "hello.txt")})
	if err := st.Execute(); err != nil {
		t.Fatalf("fs stat local agent: %v", err)
	}
	nested := filepath.Join(gdir, "sub", "n")
	mk := cmdFsMkdir(&cfgPath)
	mk.SetArgs([]string{"--parents", "v", nested})
	if err := mk.Execute(); err != nil {
		t.Fatalf("fs mkdir local agent: %v", err)
	}
	rm := cmdFsRm(&cfgPath)
	rm.SetArgs([]string{"--recursive", "v", filepath.Join(gdir, "sub")})
	if err := rm.Execute(); err != nil {
		t.Fatalf("fs rm local agent: %v", err)
	}
}

func TestCmdFsRemoteErrorPaths(t *testing.T) {
	// Bad config path (directory).
	dir := t.TempDir()
	p := dir
	for _, mk := range []func(*string) *cobra.Command{cmdFsLs, cmdFsStat, cmdFsMkdir, cmdFsRm} {
		cmd := mk(&p)
		cmd.SetArgs([]string{"vm", "/tmp"})
		if err := cmd.Execute(); err == nil {
			t.Fatal("expected config error")
		}
	}

	// Auth fail for non-loopback remote.
	apiURLFlag = "http://example.com:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	cmd := cmdFsLs(&cfg)
	cmd.SetArgs([]string{"vm", "/tmp"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected remote auth error")
	}
}

func TestCmdFsRemoteAPIErrors(t *testing.T) {
	srv := mockDaemon(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "sbox-1", Status: "running"}})
		default:
			http.Error(w, "nope", 500)
		}
	})
	useRemoteAPI(t, srv.URL)
	cfg := ""
	for _, tc := range []struct {
		name string
		mk   func(*string) *cobra.Command
		args []string
	}{
		{"ls", cmdFsLs, []string{"sbox-1", "/tmp"}},
		{"stat", cmdFsStat, []string{"sbox-1", "/tmp/a"}},
		{"mkdir", cmdFsMkdir, []string{"sbox-1", "/tmp/n"}},
		{"rm", cmdFsRm, []string{"sbox-1", "/tmp/n"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cmd := tc.mk(&cfg)
			cmd.SetArgs(tc.args)
			if err := cmd.Execute(); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
