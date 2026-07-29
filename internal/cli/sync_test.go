package cli

import (
	"bytes"
	"strings"
	"testing"

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
}
