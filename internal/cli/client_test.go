package cli

import (
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestEffectiveAPIURLPriority(t *testing.T) {
	// Not parallel: mutates package-level apiURLFlag and process env.
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("GRAIN_API", "http://env:1")
		apiURLFlag = "http://flag:2"
		t.Cleanup(func() { apiURLFlag = "" })
		cfg := config.Config{APIURL: "http://cfg:3"}
		if got := effectiveAPIURL(cfg); got != "http://flag:2" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("env over config", func(t *testing.T) {
		t.Setenv("GRAIN_API", "http://env:9")
		apiURLFlag = ""
		cfg := config.Config{APIURL: "http://cfg:3"}
		if got := effectiveAPIURL(cfg); got != "http://env:9" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("config only", func(t *testing.T) {
		t.Setenv("GRAIN_API", "")
		apiURLFlag = ""
		cfg := config.Config{APIURL: "sandbox:7474"}
		if got := effectiveAPIURL(cfg); got != "http://sandbox:7474" {
			t.Fatalf("got %q", got)
		}
	})
	t.Run("empty local", func(t *testing.T) {
		t.Setenv("GRAIN_API", "")
		apiURLFlag = ""
		if got := effectiveAPIURL(config.Config{}); got != "" {
			t.Fatalf("got %q", got)
		}
	})
}

func TestRequireRemoteAuth(t *testing.T) {
	t.Setenv("GRAIN_TOKEN", "")
	apiURLFlag = "http://10.0.0.5:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	err := requireRemoteAuth(config.Config{})
	if err == nil {
		t.Fatal("expected token required")
	}
	t.Setenv("GRAIN_TOKEN", "secret")
	if err := requireRemoteAuth(config.Config{}); err != nil {
		t.Fatal(err)
	}
	apiURLFlag = "http://127.0.0.1:7474"
	t.Setenv("GRAIN_TOKEN", "")
	if err := requireRemoteAuth(config.Config{}); err != nil {
		t.Fatalf("loopback should not require token: %v", err)
	}
}

func TestRequireLocalDaemon(t *testing.T) {
	t.Setenv("GRAIN_API", "")
	apiURLFlag = ""
	if err := requireLocalDaemon(config.Config{}, "up"); err != nil {
		t.Fatal(err)
	}
	apiURLFlag = "http://10.0.0.1:7474"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_TOKEN", "x")
	if err := requireLocalDaemon(config.Config{}, "grain up"); err == nil {
		t.Fatal("expected local-only error")
	}
}

func TestClientFromRemoteBase(t *testing.T) {
	apiURLFlag = "http://127.0.0.1:17474"
	t.Cleanup(func() { apiURLFlag = "" })
	c, err := clientFrom(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Base != "http://127.0.0.1:17474" {
		t.Fatalf("base %q", c.Base)
	}
}
