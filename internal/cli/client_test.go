package cli

import (
	"bytes"
	"io"
	"os"
	"strings"
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

func TestShouldWarnInsecureHTTP(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		base        string
		insecureEnv string
		want        bool
	}{
		{"empty", "", "", false},
		{"loopback http", "http://127.0.0.1:7474", "", false},
		{"localhost http", "http://localhost:7474", "", false},
		{"loopback bare hostport", "127.0.0.1:7474", "", false},
		{"remote http", "http://10.0.0.5:7474", "", true},
		{"remote bare hostport becomes http", "sandbox.example:7474", "", true},
		{"remote https", "https://sandbox.example:7474", "", false},
		{"remote http silenced", "http://10.0.0.5:7474", "1", false},
		{"remote http silenced true", "http://10.0.0.5:7474", "true", false},
		{"remote http silenced yes", "http://10.0.0.5:7474", "yes", false},
		{"remote http insecure empty still warns", "http://10.0.0.5:7474", "", true},
		{"remote http insecure 0 still warns", "http://10.0.0.5:7474", "0", true},
		{"ipv6 loopback", "http://[::1]:7474", "", false},
		{"https non-loopback", "https://10.0.0.5:7474", "", false},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := shouldWarnInsecureHTTP(tc.base, tc.insecureEnv); got != tc.want {
				t.Fatalf("shouldWarnInsecureHTTP(%q, %q)=%v want %v", tc.base, tc.insecureEnv, got, tc.want)
			}
		})
	}
}

func TestWarnInsecureRemoteHTTP_messageAndOnce(t *testing.T) {
	// Not parallel: mutates os.Stderr and process env; Once is process-global.
	t.Setenv("GRAIN_INSECURE_HTTP", "")

	old := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	warnInsecureRemoteHTTP("http://192.168.1.10:7474")
	warnInsecureRemoteHTTP("http://192.168.1.10:7474") // second call must not print again

	_ = w.Close()
	os.Stderr = old
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	_ = r.Close()
	out := buf.String()

	if !strings.Contains(out, "cleartext HTTP") {
		t.Fatalf("expected cleartext warning, got %q", out)
	}
	if !strings.Contains(out, "GRAIN_INSECURE_HTTP") {
		t.Fatalf("expected silence env mention, got %q", out)
	}
	if strings.Count(out, "warning:") != 1 {
		t.Fatalf("expected exactly one warning line, got %q", out)
	}

	t.Setenv("GRAIN_INSECURE_HTTP", "1")
	if shouldWarnInsecureHTTP("http://10.0.0.1:1", os.Getenv("GRAIN_INSECURE_HTTP")) {
		t.Fatal("expected silence when GRAIN_INSECURE_HTTP=1")
	}
}

func TestClientFromHTTPSBase(t *testing.T) {
	// https:// non-loopback requires a token; client construction should succeed
	// and preserve the https base (TLS via default transport).
	t.Setenv("GRAIN_TOKEN", "secret")
	t.Setenv("GRAIN_INSECURE_HTTP", "")
	apiURLFlag = "https://sandbox.example:8443"
	t.Cleanup(func() { apiURLFlag = "" })
	c, err := clientFrom(config.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if c.Base != "https://sandbox.example:8443" {
		t.Fatalf("base %q", c.Base)
	}
	if c.Token != "secret" {
		t.Fatalf("token %q", c.Token)
	}
}
