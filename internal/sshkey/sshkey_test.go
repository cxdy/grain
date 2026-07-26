package sshkey_test

import (
	"os"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/sshkey"
)

func TestEnsureIdempotent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p1, pub1, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(pub1, "ssh-ed25519 ") {
		t.Fatalf("pub %q", pub1)
	}
	if _, err := os.Stat(p1); err != nil {
		t.Fatal(err)
	}
	p2, pub2, err := sshkey.Ensure(dir)
	if err != nil {
		t.Fatal(err)
	}
	if p1 != p2 || pub1 != pub2 {
		t.Fatal("keys should be stable")
	}
}
