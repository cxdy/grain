package cli

import (
	"encoding/json"
	"net/http"
	"testing"

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
