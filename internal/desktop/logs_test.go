package desktop

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogPath(t *testing.T) {
	t.Parallel()
	p, err := LogPath("/data", "vm1", LogSerial)
	if err != nil || p != filepath.Join("/data", "vms", "vm1", "serial.log") {
		t.Fatalf("%q %v", p, err)
	}
	p, err = LogPath("/data", "vm1", LogQEMU)
	if err != nil || p != filepath.Join("/data", "logs", "vm1.log") {
		t.Fatalf("%q %v", p, err)
	}
	if _, err := LogPath("/data", "", LogSerial); err == nil {
		t.Fatal("empty name")
	}
	if _, err := LogPath("", "vm", LogSerial); err == nil {
		t.Fatal("empty data")
	}
	if _, err := LogPath("/data", "vm", LogSource("other")); err == nil {
		t.Fatal("bad source")
	}
	// path escape
	p, err = LogPath("/data", "../etc/passwd", LogSerial)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(p, "..") {
		t.Fatalf("escaped? %q", p)
	}
}

func TestReadLogs(t *testing.T) {
	dir := t.TempDir()
	vmDir := filepath.Join(dir, "vms", "s1")
	if err := os.MkdirAll(vmDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(vmDir, "serial.log")
	body := "line1\nline2\nline3\n"
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := ReadLogs(dir, "s1", LogSerial, 0)
	if err != nil || res.Missing || res.Content != body {
		t.Fatalf("%+v %v", res, err)
	}

	// truncated tail
	big := strings.Repeat("x", 1000) + "\nTAIL\n"
	if err := os.WriteFile(path, []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = ReadLogs(dir, "s1", LogSerial, 20)
	if err != nil || !res.Truncated {
		t.Fatalf("%+v %v", res, err)
	}
	if !strings.Contains(res.Content, "TAIL") {
		t.Fatalf("content %q", res.Content)
	}

	missing, err := ReadLogs(dir, "nope", LogSerial, 100)
	if err != nil || !missing.Missing {
		t.Fatalf("%+v %v", missing, err)
	}
}

func TestLogsDataDirAndCanRead(t *testing.T) {
	t.Parallel()
	conn := Connection{Name: "local", DataDir: "/custom"}
	if LogsDataDir(conn, "/default") != "/custom" {
		t.Fatal(LogsDataDir(conn, "/default"))
	}
	if LogsDataDir(Connection{}, "/default") != "/default" {
		t.Fatal("fallback")
	}
	if !CanReadLocalLogs(Connection{Name: "local"}) {
		t.Fatal("local")
	}
	if CanReadLocalLogs(Connection{API: "http://x"}) {
		t.Fatal("remote")
	}
}
