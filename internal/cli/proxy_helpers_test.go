package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestReadPIDAndPortOf(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	p := filepath.Join(dir, "x.pid")
	if _, err := readPID(p); err == nil {
		t.Fatal("missing")
	}
	if err := os.WriteFile(p, []byte("not-a-pid\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readPID(p); err == nil {
		t.Fatal("bad pid")
	}
	if err := os.WriteFile(p, []byte("12345\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	n, err := readPID(p)
	if err != nil || n != 12345 {
		t.Fatalf("%d %v", n, err)
	}
	if !pidAlive(os.Getpid()) {
		t.Fatal("self")
	}
	if pidAlive(999999991) {
		t.Fatal("dead")
	}
	if pidAlive(0) {
		t.Fatal("zero")
	}
	if portOf("0.0.0.0:3128") != "3128" {
		t.Fatal(portOf("0.0.0.0:3128"))
	}
	if portOf(":8080") != "8080" {
		t.Fatal(portOf(":8080"))
	}
	_ = portOf("nope")
}
