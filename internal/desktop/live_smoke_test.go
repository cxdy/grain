package desktop

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Live smoke against the developer's real ~/.grain when GRAIN_DESKTOP_LIVE=1.
func TestLiveDaemonSmoke(t *testing.T) {
	if os.Getenv("GRAIN_DESKTOP_LIVE") != "1" {
		t.Skip("set GRAIN_DESKTOP_LIVE=1 to run")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(filepath.Join(home, ".grain", "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	svc := NewService(cfg)
	svc.HealthWait = 5 * time.Second
	ctx := context.Background()
	res, hs, err := svc.EnsureReady(ctx)
	if err != nil {
		t.Fatalf("EnsureReady: %v res=%+v hs=%+v", err, res, hs)
	}
	if !hs.Healthy {
		t.Fatalf("unhealthy: %+v", hs)
	}
	list, err := svc.ListSandboxes(ctx)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("healthy via dial; %d sandbox(es)", len(list))
	for _, s := range list {
		t.Logf("  %s %s", s.Name, s.Status)
	}
	if len(list) == 0 {
		t.Log("no sandboxes (ok if empty host)")
	}
}
