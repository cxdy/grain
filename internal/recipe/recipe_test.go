package recipe

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/vm"
)

func TestParseAndValidateMinimal(t *testing.T) {
	t.Parallel()
	const y = `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  image: grain-ubuntu
  cpus: 2
`
	f, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "lab" || c.Image != "grain-ubuntu" || c.CPUs != 2 {
		t.Fatalf("%+v", c)
	}
	if c.HasBootstrap {
		t.Fatal("expected no bootstrap")
	}
	if c.Wait != "" {
		t.Fatalf("wait %q", c.Wait)
	}
}

func TestCompileBootstrapDefaultWait(t *testing.T) {
	t.Parallel()
	const y = `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: node-dev
spec:
  bootstrap:
    steps:
      - name: packages
        message: installing git
        run: |
          true
`
	f, err := Parse([]byte(y))
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if !c.HasBootstrap {
		t.Fatal("expected bootstrap")
	}
	if c.Wait != vm.WaitBootstrap {
		t.Fatalf("wait %q", c.Wait)
	}
	if c.ReadyName != "node-dev" {
		t.Fatalf("ready_name %q", c.ReadyName)
	}
	if !strings.Contains(c.Userdata, "#cloud-config") {
		t.Fatalf("userdata:\n%s", c.Userdata)
	}
	if !strings.Contains(c.Userdata, "grain_ready_report") {
		t.Fatalf("missing report helper:\n%s", c.Userdata)
	}
	if !strings.Contains(c.Userdata, "packages") {
		t.Fatalf("missing phase:\n%s", c.Userdata)
	}
	if !strings.Contains(c.Userdata, "installing git") {
		t.Fatalf("missing message:\n%s", c.Userdata)
	}
	if !strings.Contains(c.Userdata, "node-dev") {
		t.Fatalf("missing ready_name:\n%s", c.Userdata)
	}
	if !strings.Contains(c.Userdata, "userdata-ran") {
		t.Fatalf("missing userdata-ran:\n%s", c.Userdata)
	}
	if c.Tags["recipe"] != "true" || c.Tags["recipe_name"] != "node-dev" {
		t.Fatalf("tags %+v", c.Tags)
	}
}

func TestValidateRejects(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		yaml string
		want string
	}{
		{"no api", "kind: Sandbox\n", "apiVersion"},
		{"bad api", "apiVersion: v0\nkind: Sandbox\n", "unsupported apiVersion"},
		{"bad kind", "apiVersion: grain/v1\nkind: Pod\n", "kind"},
		{"step no run", "apiVersion: grain/v1\nkind: Sandbox\nspec:\n  bootstrap:\n    steps:\n      - name: x\n        run: \"\"\n", "run"},
		{"step bad name", "apiVersion: grain/v1\nkind: Sandbox\nspec:\n  bootstrap:\n    steps:\n      - name: \"has space\"\n        run: true\n", "single token"},
		{"bad wait", "apiVersion: grain/v1\nkind: Sandbox\nspec:\n  wait: nope\n", "wait"},
		{"bad timeout", "apiVersion: grain/v1\nkind: Sandbox\nspec:\n  ready_timeout: forever\n", "ready_timeout"},
		{"both userdata", "apiVersion: grain/v1\nkind: Sandbox\nspec:\n  userdata: x\n  userdata_file: y\n", "only one"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := Parse([]byte(tc.yaml))
			if err == nil {
				t.Fatal("expected error")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err %v want substring %q", err, tc.want)
			}
		})
	}
}

func TestLoadUserdataFileAndMounts(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	udPath := filepath.Join(dir, "extra.yaml")
	if err := os.WriteFile(udPath, []byte("#cloud-config\npackages:\n  - curl\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	recipePath := filepath.Join(dir, "lab.recipe.yaml")
	body := `
apiVersion: grain/v1
kind: Sandbox
metadata:
  name: lab
spec:
  userdata_file: extra.yaml
  mounts:
    - host: .
      guest: /work
  forwards:
    - guest_port: 3000
  bootstrap:
    ready_name: lab-ready
    steps:
      - name: ok
        run: echo hi
`
	if err := os.WriteFile(recipePath, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	f, err := Load(recipePath)
	if err != nil {
		t.Fatal(err)
	}
	c, err := f.Compile()
	if err != nil {
		t.Fatal(err)
	}
	if c.ReadyName != "lab-ready" {
		t.Fatalf("ready %q", c.ReadyName)
	}
	if len(c.Mounts) != 1 || c.Mounts[0].Guest != "/work" {
		t.Fatalf("mounts %+v", c.Mounts)
	}
	if !filepath.IsAbs(c.Mounts[0].Host) {
		t.Fatalf("host should be abs: %q", c.Mounts[0].Host)
	}
	if len(c.Forwards) != 1 || c.Forwards[0].GuestPort != 3000 {
		t.Fatalf("fwds %+v", c.Forwards)
	}
	if !strings.Contains(c.Userdata, "curl") {
		t.Fatalf("expected packages merge:\n%s", c.Userdata)
	}
	if !strings.Contains(c.Userdata, "grain_ready_report") {
		t.Fatalf("expected bootstrap:\n%s", c.Userdata)
	}
}

func TestShellSingleQuote(t *testing.T) {
	t.Parallel()
	if got := shellSingleQuote(`it's`); got != `'it'"'"'s'` {
		t.Fatalf("%q", got)
	}
}
