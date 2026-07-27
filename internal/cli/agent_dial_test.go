package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/api"
)

func TestDaemonHTTPNoToken(t *testing.T) {
	t.Parallel()
	base := &http.Client{}
	c := &api.Client{HTTP: base, Token: ""}
	got := daemonHTTP(c)
	if got != base {
		t.Fatal("expected same client when no token")
	}
}

func TestDaemonHTTPNilBase(t *testing.T) {
	t.Parallel()
	c := &api.Client{Token: ""}
	got := daemonHTTP(c)
	if got == nil {
		t.Fatal("nil client")
	}
}

func TestBearerRT(t *testing.T) {
	var sawAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sawAuth = r.Header.Get("Authorization")
		w.WriteHeader(200)
		_, _ = w.Write([]byte("ok"))
	}))
	defer srv.Close()

	c := &api.Client{
		Base:  srv.URL,
		Token: "sekret",
		HTTP:  &http.Client{},
	}
	hc := daemonHTTP(c)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/x", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	_, _ = io.ReadAll(res.Body)
	if sawAuth != "Bearer sekret" {
		t.Fatalf("auth=%q", sawAuth)
	}
}

func TestDialGuestAgentForceErrors(t *testing.T) {
	// Get fails → force error / skip
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/vms/") {
			http.Error(w, `{"error":"not found"}`, 404)
			return
		}
		w.WriteHeader(200)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}

	if _, err := dialGuestAgent(c, "missing", false); err != errAgentSkip {
		t.Fatalf("want errAgentSkip, got %v", err)
	}
	if _, err := dialGuestAgent(c, "missing", true); err == nil {
		t.Fatal("force should error")
	}

	// VM without agent endpoint
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "noagent", "status": "running", "agent_port": 0, "agent_cid": 0,
		})
	}))
	defer srv2.Close()
	c2 := &api.Client{Base: srv2.URL, HTTP: srv2.Client()}
	if _, err := dialGuestAgent(c2, "noagent", false); err != errAgentSkip {
		t.Fatalf("skip: %v", err)
	}
	if _, err := dialGuestAgent(c2, "noagent", true); err == nil || !strings.Contains(err.Error(), "agent not available") {
		t.Fatalf("force no endpoint: %v", err)
	}
}

func TestExecViaDaemonAPIBufferedFallback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/exec") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("buffered") == "false" {
			http.Error(w, "stream fail", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"stdout": "hi\n", "stderr": "warn\n", "exit_code": 0,
		})
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := execViaDaemonAPI(c, "vm1", []string{"echo", "hi"}); err != nil {
		t.Fatal(err)
	}
}

func TestExecViaDaemonAPIStreamOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/exec") {
			http.NotFound(w, r)
			return
		}
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"type":"stdout","data":"out\n"}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"stderr","data":"err\n"}` + "\n"))
			_, _ = w.Write([]byte(`{"type":"exit","exit_code":0}` + "\n"))
			return
		}
		http.Error(w, "unexpected buffered", 500)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := execViaDaemonAPI(c, "vm1", []string{"true"}); err != nil {
		t.Fatal(err)
	}
}

func TestExecViaDaemonAPINonZero(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("buffered") == "false" {
			w.Header().Set("Content-Type", "application/x-ndjson")
			_, _ = w.Write([]byte(`{"type":"exit","exit_code":3}` + "\n"))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	err := execViaDaemonAPI(c, "vm1", []string{"false"})
	if err == nil {
		t.Fatal("expected exit code error")
	}
	if ec, ok := err.(exitCodeError); !ok || int(ec) != 3 {
		t.Fatalf("got %v", err)
	}
}

func TestExecViaAgentViaDaemonFlag(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/exec") {
			if r.URL.Query().Get("buffered") == "false" {
				w.Header().Set("Content-Type", "application/x-ndjson")
				_, _ = w.Write([]byte(`{"type":"exit","exit_code":0}` + "\n"))
				return
			}
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := execViaAgent(c, "vm", []string{"true"}, false, true); err != nil {
		t.Fatal(err)
	}
}

func TestCpViaAgentPolicyErrors(t *testing.T) {
	t.Parallel()
	c := &api.Client{}
	err := cpViaAgent(c, cpSpec{Guest: true, Name: "a", Path: "/x"}, cpSpec{Guest: true, Name: "a", Path: "/y"}, false, false)
	if err == nil || !strings.Contains(err.Error(), "same VM") {
		t.Fatalf("same vm: %v", err)
	}
	err = cpViaAgent(c, cpSpec{Guest: true, Name: "a", Path: "/x"}, cpSpec{Guest: true, Name: "b", Path: "/y"}, false, false)
	if err == nil || !strings.Contains(err.Error(), "guest-to-guest") {
		t.Fatalf("g2g: %v", err)
	}
	err = cpViaAgent(c, cpSpec{Path: "/local"}, cpSpec{Path: "/other"}, false, false)
	if err != errAgentSkip {
		t.Fatalf("host-host: %v", err)
	}
}

func TestDialGuestAgentUnreachablePort(t *testing.T) {
	// Agent port set but nothing listening → Dial fails
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"name": "vm", "status": "running", "agent_port": 1, "agent_cid": 0,
		})
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if _, err := dialGuestAgent(c, "vm", false); err != errAgentSkip {
		t.Fatalf("want skip on dial fail, got %v", err)
	}
	if _, err := dialGuestAgent(c, "vm", true); err == nil {
		t.Fatal("force should fail")
	}
}

func TestDaemonPutGetFile(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/src.txt"
	if err := os.WriteFile(src, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}

	var putPath string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && strings.Contains(r.URL.Path, "/cp"):
			putPath = r.URL.Query().Get("path")
			b, _ := io.ReadAll(r.Body)
			gotBody = b
			w.WriteHeader(204)
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/fs/stat"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"name": "src.txt", "type": "file", "mode": "0644", "size": 7,
			})
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/cp"):
			_, _ = w.Write([]byte("payload"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}

	ctx := context.Background()
	if err := daemonPut(ctx, c, "vm", src, "/guest/src.txt"); err != nil {
		t.Fatalf("put: %v", err)
	}
	if putPath != "/guest/src.txt" || string(gotBody) != "payload" {
		t.Fatalf("putPath=%q body=%q", putPath, gotBody)
	}

	dst := dir + "/out.txt"
	if err := daemonGet(ctx, c, "vm", "/guest/src.txt", dst); err != nil {
		t.Fatalf("get: %v", err)
	}
	b, err := os.ReadFile(dst)
	if err != nil || string(b) != "payload" {
		t.Fatalf("got %q %v", b, err)
	}
}

func TestDaemonPutDirSuffix(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/file.bin"
	if err := os.WriteFile(src, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	var putPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putPath = r.URL.Query().Get("path")
			_, _ = io.ReadAll(r.Body)
			w.WriteHeader(204)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	if err := daemonPut(context.Background(), c, "vm", src, "/dest/"); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(putPath, "file.bin") {
		t.Fatalf("path %q", putPath)
	}
}

func TestCpViaAgentDaemonMode(t *testing.T) {
	dir := t.TempDir()
	src := dir + "/a.txt"
	if err := os.WriteFile(src, []byte("z"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(204)
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()
	c := &api.Client{Base: srv.URL, HTTP: srv.Client()}
	err := cpViaAgent(c,
		cpSpec{Path: src},
		cpSpec{Guest: true, Name: "vm", Path: "/tmp/a.txt"},
		false, true,
	)
	if err != nil {
		t.Fatal(err)
	}
}
