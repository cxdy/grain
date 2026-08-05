package daemon

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestRemoveRuntimeFilesIfOwned(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	socket := filepath.Join(dir, "grain.sock")
	pidPath := filepath.Join(dir, "grain.pid")
	if err := os.WriteFile(socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	self := os.Getpid()

	// Own the pid file → both removed.
	if err := os.WriteFile(pidPath, []byte(strconv.Itoa(self)+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeRuntimeFilesIfOwned(socket, pidPath, self, nil)
	if _, err := os.Stat(socket); !os.IsNotExist(err) {
		t.Fatal("owned socket should be removed")
	}
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("owned pid should be removed")
	}

	// Recreate; pid file belongs to someone else → leave alone.
	if err := os.WriteFile(socket, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pidPath, []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	removeRuntimeFilesIfOwned(socket, pidPath, self, nil)
	if _, err := os.Stat(socket); err != nil {
		t.Fatal("successor socket must stay")
	}
	if _, err := os.Stat(pidPath); err != nil {
		t.Fatal("successor pid must stay")
	}

	// Missing pid file → do not touch socket.
	_ = os.Remove(pidPath)
	removeRuntimeFilesIfOwned(socket, pidPath, self, nil)
	if _, err := os.Stat(socket); err != nil {
		t.Fatal("socket must stay when pid file is missing")
	}
}
