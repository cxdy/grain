package cli

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/secrets"
	"github.com/spf13/cobra"
)

func TestParsePublishSocketFlag(t *testing.T) {
	t.Parallel()
	h, g, err := parsePublishSocketFlag("/tmp/docker.sock:/var/run/docker.sock")
	if err != nil {
		t.Fatal(err)
	}
	if h != "/tmp/docker.sock" || g != "/var/run/docker.sock" {
		t.Fatalf("got %q %q", h, g)
	}
	// host path may contain colons — last colon splits
	h, g, err = parsePublishSocketFlag("/weird:path:/guest/sock")
	if err != nil {
		t.Fatal(err)
	}
	if h != "/weird:path" || g != "/guest/sock" {
		t.Fatalf("got %q %q", h, g)
	}
	for _, bad := range []string{"", "nocolon", ":guest", "host:", "host:rel", ":/g"} {
		if _, _, err := parsePublishSocketFlag(bad); err == nil {
			t.Fatalf("expected error for %q", bad)
		}
	}
}

func TestParsePublishSocketFlags(t *testing.T) {
	t.Parallel()
	out, err := parsePublishSocketFlags(nil)
	if err != nil || out != nil {
		t.Fatalf("nil: %v %v", out, err)
	}
	out, err = parsePublishSocketFlags([]string{"/a:/b", "/c:/d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 2 || out[0].Host != "/a" || out[1].Guest != "/d" {
		t.Fatalf("%+v", out)
	}
	if _, err := parsePublishSocketFlags([]string{"bad"}); err == nil {
		t.Fatal("expected error")
	}
}

func TestCmdSecretSetValidation(t *testing.T) {
	cfg := ""
	cmd := cmdSecretSet(&cfg)
	// invalid name
	cmd.SetArgs([]string{"!!!", "--value", "x"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "invalid secret name") {
		t.Fatalf("invalid name: %v", err)
	}
	// both from-file and value
	cmd = cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"okname", "--value", "x", "--from-file", "/tmp/x"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "only one of") {
		t.Fatalf("both: %v", err)
	}
	// neither
	cmd = cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"okname"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "provide --from-file") {
		t.Fatalf("neither: %v", err)
	}
}

func TestCmdSecretWithDaemon(t *testing.T) {
	// Not parallel: mutates apiURLFlag.
	var secretsList []secrets.Meta
	var lastPut secrets.PutRequest
	var deleted string
	var injectVM, injectSecret, injectPath string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/secrets":
			_ = json.NewEncoder(w).Encode(secretsList)
		case r.Method == http.MethodPost && r.URL.Path == "/secrets":
			_ = json.NewDecoder(r.Body).Decode(&lastPut)
			m := secrets.Meta{Name: lastPut.Name, Mode: lastPut.Mode, Size: 3, UpdatedAt: time.Now().UTC()}
			if m.Mode == "" {
				m.Mode = "0600"
			}
			_ = json.NewEncoder(w).Encode(m)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/secrets/"):
			deleted = strings.TrimPrefix(r.URL.Path, "/secrets/")
			w.WriteHeader(204)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/secrets/"):
			// /vms/{vm}/secrets/{name}
			parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
			if len(parts) >= 4 {
				injectVM = parts[1]
				injectSecret = parts[3]
			}
			if r.Body != nil {
				var body map[string]string
				_ = json.NewDecoder(r.Body).Decode(&body)
				injectPath = body["path"]
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"path": "/run/grain/secrets/" + injectSecret, "mode": "0600"})
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "sbox-1", "status": "running"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")

	cfg := ""

	// ls empty
	secretsList = nil
	cmd := cmdSecretLs(&cfg)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls empty: %v", err)
	}

	// ls with items
	secretsList = []secrets.Meta{{Name: "tok", Mode: "0600", Size: 8, UpdatedAt: time.Now().UTC()}}
	cmd = cmdSecretLs(&cfg)
	if err := cmd.Execute(); err != nil {
		t.Fatalf("ls: %v", err)
	}

	// set --value
	cmd = cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"mytok", "--value", "abc", "--mode", "0400"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("set value: %v", err)
	}
	if lastPut.Name != "mytok" {
		t.Fatalf("put name %q", lastPut.Name)
	}
	raw, _ := base64.StdEncoding.DecodeString(lastPut.DataBase64)
	if string(raw) != "abc" {
		t.Fatalf("data %q", raw)
	}

	// set --from-file
	dir := t.TempDir()
	fp := filepath.Join(dir, "secret.txt")
	if err := os.WriteFile(fp, []byte("fromfile"), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd = cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"filetok", "--from-file", fp})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("set file: %v", err)
	}
	raw, _ = base64.StdEncoding.DecodeString(lastPut.DataBase64)
	if string(raw) != "fromfile" {
		t.Fatalf("file data %q", raw)
	}

	// rm
	cmd = cmdSecretRm(&cfg)
	cmd.SetArgs([]string{"mytok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("rm: %v", err)
	}
	if deleted != "mytok" {
		t.Fatalf("deleted %q", deleted)
	}

	// inject with explicit vm
	cmd = cmdSecretInject(&cfg)
	cmd.SetArgs([]string{"--path", "/tmp/s", "sbox-1", "mytok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inject: %v", err)
	}
	if injectVM != "sbox-1" || injectSecret != "mytok" || injectPath != "/tmp/s" {
		t.Fatalf("inject %q %q %q", injectVM, injectSecret, injectPath)
	}
}

func TestCmdSecretErrorPaths(t *testing.T) {
	dir := t.TempDir()
	// loadCfg fails when path is a directory
	cases := []struct {
		name string
		cmd  *cobra.Command
	}{
		{"ls", cmdSecretLs(&dir)},
		{"set", cmdSecretSet(&dir)},
		{"rm", cmdSecretRm(&dir)},
		{"inject", cmdSecretInject(&dir)},
	}
	for _, tc := range cases {
		cmd := tc.cmd
		switch tc.name {
		case "set":
			cmd.SetArgs([]string{"okname", "--value", "x"})
		case "rm":
			cmd.SetArgs([]string{"okname"})
		case "inject":
			cmd.SetArgs([]string{"vm", "sec"})
		}
		if err := cmd.Execute(); err == nil {
			t.Fatalf("expected config error for %s", tc.name)
		}
	}

	// remote auth
	apiURLFlag = "http://example.com:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	if err := cmdSecretLs(&cfg).Execute(); err == nil {
		t.Fatal("expected auth error")
	}
}

func TestCmdSecretDaemonBranches(t *testing.T) {
	var secretsList []secrets.Meta
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.Method == http.MethodGet && r.URL.Path == "/secrets":
			_ = json.NewEncoder(w).Encode(secretsList)
		case r.Method == http.MethodPost && r.URL.Path == "/secrets":
			http.Error(w, "put fail", 500)
		case r.Method == http.MethodDelete && strings.HasPrefix(r.URL.Path, "/secrets/"):
			http.Error(w, "del fail", 500)
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/secrets/"):
			// inject — return empty path to hit default
			_ = json.NewEncoder(w).Encode(map[string]string{"mode": "0600"})
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "only", "status": "running"}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""

	// ls with empty Mode field
	secretsList = []secrets.Meta{{Name: "x", Mode: "", Size: 1, UpdatedAt: time.Now().UTC()}}
	if err := cmdSecretLs(&cfg).Execute(); err != nil {
		t.Fatal(err)
	}

	// set fails at API
	cmd := cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"tok", "--value", "v"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected set error")
	}

	// rm fails at API
	cmd = cmdSecretRm(&cfg)
	cmd.SetArgs([]string{"tok"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected rm error")
	}

	// inject single-arg resolves only VM; empty path uses default
	cmd = cmdSecretInject(&cfg)
	cmd.SetArgs([]string{"tok"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("inject: %v", err)
	}
}

func TestCmdSecretHealthDown(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "down", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	if err := cmdSecretLs(&cfg).Execute(); err == nil {
		t.Fatal("expected health error")
	}
	cmd := cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"a", "--value", "b"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected health error")
	}
	cmd = cmdSecretRm(&cfg)
	cmd.SetArgs([]string{"a"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected health error")
	}
	cmd = cmdSecretInject(&cfg)
	cmd.SetArgs([]string{"vm", "a"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected health error")
	}
}

func TestCmdSecretClientFromAndInjectResolve(t *testing.T) {
	apiURLFlag = "http://example.com:9"
	t.Cleanup(func() { apiURLFlag = "" })
	t.Setenv("GRAIN_API", "")
	t.Setenv("GRAIN_TOKEN", "")
	cfg := ""
	// clientFrom auth fail for set/rm/inject
	cmd := cmdSecretSet(&cfg)
	cmd.SetArgs([]string{"n", "--value", "v"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("set auth")
	}
	cmd = cmdSecretRm(&cfg)
	cmd.SetArgs([]string{"n"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("rm auth")
	}
	cmd = cmdSecretInject(&cfg)
	cmd.SetArgs([]string{"vm", "n"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("inject auth")
	}

	// inject resolve fails with multi VM
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/healthz":
			w.WriteHeader(200)
		case "/vms":
			_ = json.NewEncoder(w).Encode([]map[string]any{
				{"name": "a", "status": "running"},
				{"name": "b", "status": "running"},
			})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	apiURLFlag = srv.URL
	cmd = cmdSecretInject(&cfg)
	cmd.SetArgs([]string{"tok"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected resolve error")
	}

	// inject API error
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/healthz":
			w.WriteHeader(200)
		case r.URL.Path == "/vms":
			_ = json.NewEncoder(w).Encode([]map[string]any{{"name": "only", "status": "running"}})
		case strings.Contains(r.URL.Path, "/secrets/"):
			http.Error(w, "inj", 500)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv2.Close()
	apiURLFlag = srv2.URL
	cmd = cmdSecretInject(&cfg)
	cmd.SetArgs([]string{"only", "tok"})
	if err := cmd.Execute(); err == nil {
		t.Fatal("expected inject error")
	}
}

func TestParsePublishSocketEmptyHostEdge(t *testing.T) {
	// host empty after split is hard via LastIndex rules; cover flags error path already.
	// " :/g" trims to ":/g" which fails i<=0.
	if _, _, err := parsePublishSocketFlag(" :/g"); err == nil {
		t.Fatal("expected error")
	}
}
