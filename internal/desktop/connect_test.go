package desktop

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConnectionIsLocal(t *testing.T) {
	t.Parallel()
	if !(Connection{Name: "local"}).IsLocal() {
		t.Fatal("empty API should be local")
	}
	if (Connection{API: "http://127.0.0.1:7474"}).IsLocal() {
		t.Fatal("HTTP API is remote for capability matrix")
	}
	if (Connection{API: "https://lab.example"}).IsLocal() {
		t.Fatal("remote https should not be local")
	}
}

func TestResolvedToken(t *testing.T) {
	t.Setenv("GRAIN_TEST_TOKEN_X", "from-env")
	c := Connection{Token: "plain"}
	if c.ResolvedToken() != "plain" {
		t.Fatalf("token: %q", c.ResolvedToken())
	}
	c = Connection{TokenEnv: "GRAIN_TEST_TOKEN_X"}
	if c.ResolvedToken() != "from-env" {
		t.Fatalf("token_env: %q", c.ResolvedToken())
	}
	c = Connection{}
	if c.ResolvedToken() != "" {
		t.Fatalf("empty: %q", c.ResolvedToken())
	}
}

func TestNormalizeAndLoopback(t *testing.T) {
	t.Parallel()
	if got := NormalizeAPIURL("127.0.0.1:7474"); got != "http://127.0.0.1:7474" {
		t.Fatalf("bare: %q", got)
	}
	if got := NormalizeAPIURL("http://x/"); got != "http://x" {
		t.Fatalf("trim: %q", got)
	}
	if !IsLoopbackAPI("http://127.0.0.1:7474") {
		t.Fatal("127.0.0.1")
	}
	if !IsLoopbackAPI("http://localhost:9") {
		t.Fatal("localhost")
	}
	if IsLoopbackAPI("http://192.168.1.1:7474") {
		t.Fatal("lan not loopback")
	}
	if !WarnCleartextRemote("http://example.com:7474") {
		t.Fatal("want cleartext warning")
	}
	if WarnCleartextRemote("https://example.com") {
		t.Fatal("https ok")
	}
	if WarnCleartextRemote("http://127.0.0.1:7474") {
		t.Fatal("loopback http ok")
	}
}

func TestEnsureLocalAndResolve(t *testing.T) {
	t.Parallel()
	sock := "/tmp/grain-test.sock"
	dd := "/tmp/grain-data"
	list := EnsureLocalConnection(nil, sock, dd)
	if len(list) != 1 || list[0].Name != "local" || list[0].Socket != sock {
		t.Fatalf("implicit local: %+v", list)
	}
	list = EnsureLocalConnection([]Connection{{Name: "lab", API: "http://x:1"}}, sock, dd)
	if list[0].Name != "local" || list[1].Name != "lab" {
		t.Fatalf("prepend local: %+v", list)
	}
	c, err := ResolveConnection(list, "lab", "local", sock, dd)
	if err != nil || c.Name != "lab" {
		t.Fatalf("resolve lab: %+v %v", c, err)
	}
	_, err = ResolveConnection(list, "missing", "local", sock, dd)
	if err == nil {
		t.Fatal("want unknown connection error")
	}
	c, err = ResolveConnection(list, "", "local", sock, dd)
	if err != nil || c.Name != "local" {
		t.Fatalf("default local: %+v %v", c, err)
	}
	names := ConnectionNames(list)
	if len(names) != 2 || names[0] != "local" {
		t.Fatalf("names: %v", names)
	}
}

func TestResolvedSocketExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	c := Connection{Socket: "~/grain.sock"}
	got := c.ResolvedSocket("")
	want := filepath.Join(home, "grain.sock")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestLocalConnection(t *testing.T) {
	t.Parallel()
	c := LocalConnection("/s", "/d")
	if c.Name != "local" || !c.IsLocal() || c.Socket != "/s" {
		t.Fatalf("%+v", c)
	}
}
