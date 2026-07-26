package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/cxdy/grain/internal/config"
)

func TestResolvedAPIToken(t *testing.T) {
	t.Parallel()
	c := config.Config{APIToken: "a", AuthToken: "b"}
	if c.ResolvedAPIToken() != "a" {
		t.Fatalf("prefer api_token, got %q", c.ResolvedAPIToken())
	}
	c = config.Config{AuthToken: "b"}
	if c.ResolvedAPIToken() != "b" {
		t.Fatalf("auth_token alias, got %q", c.ResolvedAPIToken())
	}
	c = config.Config{}
	if c.ResolvedAPIToken() != "" {
		t.Fatalf("empty, got %q", c.ResolvedAPIToken())
	}
}

func TestLoadAPITokenYAML(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	body := []byte("api_token: from-yaml\n")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if c.ResolvedAPIToken() != "from-yaml" {
		t.Fatalf("got %q", c.ResolvedAPIToken())
	}

	path2 := filepath.Join(dir, "config2.yaml")
	if err := os.WriteFile(path2, []byte("auth_token: alias-tok\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	c2, err := config.Load(path2)
	if err != nil {
		t.Fatal(err)
	}
	if c2.ResolvedAPIToken() != "alias-tok" {
		t.Fatalf("got %q", c2.ResolvedAPIToken())
	}
}
