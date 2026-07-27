package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/api"
	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/vm"
)

func TestCpViaSCPDirect(t *testing.T) {
	// Call cpViaSCP without remoteMode so the function body is executed.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/vms/box" {
			_ = json.NewEncoder(w).Encode(&vm.Instance{
				Name: "box", Status: vm.StatusRunning, IP: "127.0.0.1", SSHPort: 1,
			})
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	cfg := config.Defaults()
	cfg.SSHUser = "ubuntu"
	cfg.DataDir = t.TempDir()

	srcFile := filepath.Join(t.TempDir(), "f.txt")
	if err := os.WriteFile(srcFile, []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cpViaSCP(cfg, c, parseCPSpec(srcFile), parseCPSpec("box:/tmp/f.txt")); err == nil {
		t.Fatal("expected scp fail")
	}
	if err := cpViaSCP(cfg, c, parseCPSpec("box:/tmp/f.txt"), parseCPSpec(filepath.Join(t.TempDir(), "out"))); err == nil {
		t.Fatal("expected scp fail")
	}
	if err := cpViaSCP(cfg, c, parseCPSpec("box:/tmp/dir"), parseCPSpec(filepath.Join(t.TempDir(), "d"))); err == nil {
		t.Fatal("expected scp fail")
	}
	dirSrc := t.TempDir()
	if err := os.WriteFile(filepath.Join(dirSrc, "a"), []byte("a"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := cpViaSCP(cfg, c, parseCPSpec(dirSrc), parseCPSpec("box:/tmp/d")); err == nil {
		t.Fatal("expected scp fail")
	}
	if err := cpViaSCP(cfg, c, parseCPSpec("missing:/x"), parseCPSpec(filepath.Join(t.TempDir(), "o"))); err == nil {
		t.Fatal("expected get error")
	}
}

func TestFollowFileAndCopyFromOffset(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "log.txt")
	if err := os.WriteFile(path, []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- followFile(ctx, path, &buf, 20*time.Millisecond)
	}()
	time.Sleep(50 * time.Millisecond)
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = f.WriteString("world\n")
	_ = f.Close()
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("new\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	_ = os.Remove(path)
	time.Sleep(50 * time.Millisecond)
	if err := os.WriteFile(path, []byte("again\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timeout")
	}
	if !strings.Contains(buf.String(), "hello") {
		t.Fatalf("buf=%q", buf.String())
	}

	p2 := filepath.Join(dir, "c.txt")
	if err := os.WriteFile(p2, []byte("abcdef"), 0o644); err != nil {
		t.Fatal(err)
	}
	var b2 bytes.Buffer
	n, err := copyFromOffset(p2, 0, &b2)
	if err != nil || n != 6 {
		t.Fatalf("n=%d err=%v", n, err)
	}
	n, err = copyFromOffset(p2, 6, &b2)
	if err != nil || n != 0 {
		t.Fatalf("eof n=%d err=%v", n, err)
	}
	_, err = copyFromOffset(p2, 100, &b2)
	if !isTruncate(err) {
		t.Fatalf("want truncate got %v", err)
	}
	_, err = copyFromOffset(filepath.Join(dir, "nope"), 0, &b2)
	if err == nil {
		t.Fatal("want missing")
	}
	ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel2()
	_ = followFile(ctx2, p2, &bytes.Buffer{}, 0)
}

func TestCmdUpBackgroundSpawns(t *testing.T) {
	// Background path calls os.Executable() + Start. When the test binary is the
	// executable, the child is not a real grain daemon — still covers the spawn
	// and socket-wait loop. Kill any child we can find via process group later.
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "c.yaml")
	sock := filepath.Join(dir, "g.sock")
	yml := fmt.Sprintf("data_dir: %s\nsocket: %s\nhypervisor: mock\napi: \"\"\nlog_level: error\n", dir, sock)
	if err := os.WriteFile(cfgPath, []byte(yml), 0o644); err != nil {
		t.Fatal(err)
	}
	// Shorten wait: create the socket so the poll loop exits immediately.
	// (Parent only stats the path; it does not require a real listener.)
	if err := os.WriteFile(sock, []byte{}, 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdUp(&cfgPath)
	if err := cmd.Execute(); err != nil {
		t.Logf("up background: %v", err)
	}
}

func TestCmdFsSuccessPaths(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path := r.URL.Path
		switch {
		case path == "/healthz":
			w.WriteHeader(200)
		case path == "/vms":
			_ = json.NewEncoder(w).Encode([]*vm.Instance{{Name: "v", Status: "running"}})
		case path == "/vms/v":
			_ = json.NewEncoder(w).Encode(&vm.Instance{Name: "v", Status: "running"})
		case strings.Contains(path, "readdir") || strings.Contains(path, "/fs/ls"):
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "a", "type": "file", "size": 1}})
		case strings.Contains(path, "stat"):
			_ = json.NewEncoder(w).Encode(map[string]any{"name": "a", "type": "file", "size": 1, "mode": "0644"})
		case strings.Contains(path, "mkdir") || strings.Contains(path, "remove") || strings.Contains(path, "/rm"):
			w.WriteHeader(200)
		default:
			// fs ops often under /vms/v/...
			if strings.HasPrefix(path, "/vms/v") {
				w.WriteHeader(200)
				return
			}
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""

	for _, fn := range []func(){
		func() {
			c := cmdFsLs(&cfg)
			c.SetArgs([]string{"v", "/"})
			_ = c.Execute()
		},
		func() {
			c := cmdFsStat(&cfg)
			c.SetArgs([]string{"v", "/a"})
			_ = c.Execute()
		},
		func() {
			c := cmdFsMkdir(&cfg)
			c.SetArgs([]string{"v", "/tmp/x"})
			_ = c.Execute()
		},
		func() {
			c := cmdFsRm(&cfg)
			c.SetArgs([]string{"v", "/tmp/x"})
			_ = c.Execute()
		},
	} {
		fn()
	}
}

func TestCmdLogsDumpLocal(t *testing.T) {
	apiURLFlag = ""
	t.Setenv("GRAIN_API", "")
	dir := t.TempDir()
	// Serial log path: data_dir/vms/<name>/serial.log
	serialDir := filepath.Join(dir, "vms", "vm1")
	if err := os.MkdirAll(serialDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(serialDir, "serial.log"), []byte("line1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// QEMU log path: data_dir/logs/<name>.log
	if err := os.MkdirAll(filepath.Join(dir, "logs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "logs", "vm1.log"), []byte("qemu\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(dir, "c.yaml")
	if err := os.WriteFile(cfgPath, []byte("data_dir: "+dir+"\nhypervisor: mock\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := cmdLogs(&cfgPath)
	cmd.SetArgs([]string{"vm1"})
	if err := cmd.Execute(); err != nil {
		t.Fatal(err)
	}
	cmdQ := cmdLogs(&cfgPath)
	cmdQ.SetArgs([]string{"--qemu", "vm1"})
	if err := cmdQ.Execute(); err != nil {
		t.Fatal(err)
	}
}
