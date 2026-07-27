package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyOwnershipNilAndNonRoot(t *testing.T) {
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
}

func TestApplyCredentialNonRootNoop(t *testing.T) {
	t.Parallel()
	cmd := exec.Command("true")
	u := uint32(1)
	g := uint32(1)
	applyCredential(cmd, &u, &g)
	applyCredential(cmd, &u, nil)
	applyCredential(cmd, nil, &g)
	applyCredential(cmd, nil, nil)
	if os.Geteuid() != 0 && cmd.SysProcAttr != nil {
		t.Fatalf("expected no credential when non-root: %+v", cmd.SysProcAttr)
	}
}

func TestPutBinaryWithUIDGIDNonRoot(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "f")
	u := uint32(0)
	g := uint32(0)
	if err := putBinary(path, strings.NewReader("hi"), 0o644, &u, &g); err != nil {
		t.Fatal(err)
	}
}
