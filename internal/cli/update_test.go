package cli

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/update"
)

func TestUpdateCheckEnabled(t *testing.T) {
	cfgOn := config.Defaults()
	cfgOn.CheckUpdates = true
	cfgOff := config.Defaults()
	cfgOff.CheckUpdates = false

	t.Setenv("GRAIN_NO_UPDATE_CHECK", "")
	t.Setenv("GRAIN_CHECK_UPDATES", "")
	// Clear may not unset if empty string is set — LookupEnv treats "" as set.
	// updateCheckEnabled: LookupEnv with "" → envTruthy("") is false → disables!
	// So we must Unsetenv for default config path.
	t.Setenv("GRAIN_CHECK_UPDATES", "unset-me")
	_ = os.Unsetenv("GRAIN_CHECK_UPDATES")
	_ = os.Unsetenv("GRAIN_NO_UPDATE_CHECK")

	if !updateCheckEnabled(cfgOn) {
		t.Fatal("default on")
	}
	if updateCheckEnabled(cfgOff) {
		t.Fatal("config off")
	}

	t.Setenv("GRAIN_CHECK_UPDATES", "0")
	if updateCheckEnabled(cfgOn) {
		t.Fatal("env 0 disables")
	}
	t.Setenv("GRAIN_CHECK_UPDATES", "true")
	if !updateCheckEnabled(cfgOff) {
		t.Fatal("env true enables")
	}
	_ = os.Unsetenv("GRAIN_CHECK_UPDATES")
	t.Setenv("GRAIN_NO_UPDATE_CHECK", "1")
	if updateCheckEnabled(cfgOn) {
		t.Fatal("NO_UPDATE_CHECK disables")
	}
}

func TestMaybePrintUpdateNotice(t *testing.T) {
	dir := t.TempDir()
	path := update.CachePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body, err := json.Marshal(update.Cache{
		Latest:    "v9.9.9",
		CheckedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	cfg := config.Defaults()
	cfg.DataDir = dir
	cfg.CheckUpdates = true
	_ = os.Unsetenv("GRAIN_CHECK_UPDATES")
	_ = os.Unsetenv("GRAIN_NO_UPDATE_CHECK")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	old := os.Stderr
	os.Stderr = w
	maybePrintUpdateNotice(cfg, "0.1.0", "ls")
	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(r)
	out := buf.String()
	if !strings.Contains(out, "grain update") || !strings.Contains(out, "v9.9.9") {
		t.Fatalf("notice: %q", out)
	}

	// Disabled via env
	r2, w2, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w2
	t.Setenv("GRAIN_CHECK_UPDATES", "0")
	maybePrintUpdateNotice(cfg, "0.1.0", "ls")
	_ = w2.Close()
	os.Stderr = old
	var buf2 bytes.Buffer
	_, _ = buf2.ReadFrom(r2)
	if buf2.Len() != 0 {
		t.Fatalf("expected no notice: %q", buf2.String())
	}

	// Skipped command
	_ = os.Unsetenv("GRAIN_CHECK_UPDATES")
	r3, w3, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w3
	maybePrintUpdateNotice(cfg, "0.1.0", "update")
	_ = w3.Close()
	os.Stderr = old
	var buf3 bytes.Buffer
	_, _ = buf3.ReadFrom(r3)
	if buf3.Len() != 0 {
		t.Fatalf("skip update cmd: %q", buf3.String())
	}
}

func TestDisplayVer(t *testing.T) {
	t.Parallel()
	if displayVer("0.2.2") != "v0.2.2" {
		t.Fatal(displayVer("0.2.2"))
	}
	if displayVer("v0.2.2") != "v0.2.2" {
		t.Fatal(displayVer("v0.2.2"))
	}
	if displayVer("dev") != "dev" {
		t.Fatal(displayVer("dev"))
	}
}

func TestRunInstallScript(t *testing.T) {
	scriptSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/plain")
		_, _ = w.Write([]byte("#!/bin/sh\necho installed-ok\n"))
	}))
	t.Cleanup(scriptSrv.Close)
	t.Setenv("GRAIN_INSTALL_SCRIPT", scriptSrv.URL)
	if err := runInstallScript(); err != nil {
		t.Fatal(err)
	}
}

func TestCmdUpdateRegistered(t *testing.T) {
	t.Parallel()
	cfg := ""
	cmd := cmdUpdate(&cfg, "0.0.0-test")
	if cmd.Use != "update" {
		t.Fatal(cmd.Use)
	}
	if cmd.Flags().Lookup("check") == nil || cmd.Flags().Lookup("force") == nil {
		t.Fatal("flags")
	}
}

func TestRunUpdateCheckExitCode(t *testing.T) {
	// Full runUpdate hits real GitHub when ForceRefresh — use package Check only for isolation.
	// Verify exitCodeError wiring:
	var err error = exitCodeError(1)
	ec, ok := err.(interface{ ExitCode() int })
	if !ok || ec.ExitCode() != 1 {
		t.Fatal(err)
	}
}
