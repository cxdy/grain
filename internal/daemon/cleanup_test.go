package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRemovePIDFileIfOwned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	socket := filepath.Join(dir, "grain.sock")
	pidPath := filepath.Join(dir, "grain.pid")
	if err := os.WriteFile(socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	self := os.Getpid()

	// Own the pid file → pid removed, socket left for successor rebind path.
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(self)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removePIDFileIfOwned(pidPath, self, nil)
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("owned pid should be removed")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatal("socket must never be removed on shutdown")
	}

	// Pid file belongs to someone else → leave alone.
	if err := os.WriteFile(pidPath, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removePIDFileIfOwned(pidPath, self, nil)
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatal("successor pid must stay")
	}
	if _, err := os.Stat(socket); err != nil {
		t.Fatal("socket must stay")
	}
}
