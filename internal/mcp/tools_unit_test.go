package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func TestAppendProgress(t *testing.T) {
	t.Parallel()
	if p := appendProgress(nil, "stdout", ""); p != nil {
		t.Fatal(p)
	}
	long := strings.Repeat("x", 5000)
	p := appendProgress(nil, "stdout", long)
	if len(p) != 1 || !strings.Contains(p[0], "…") {
		t.Fatalf("%v", p)
	}
	// cap at 200 chunks
	var prog []string
	for i := 0; i < 250; i++ {
		prog = appendProgress(prog, "stderr", "c")
	}
	if len(prog) != 200 {
		t.Fatalf("len %d", len(prog))
	}
}

func TestBuildCreatePresetMergeAndPort(t *testing.T) {
	t.Parallel()
	s := &Server{}
	// k3s preset: default resources + 6443 publish + tags
	cr, err := s.buildCreate(createIn{
		Name:    "k",
		Preset:  "k3s",
		Publish: []string{"80:80"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if cr.CPUs <= 0 || cr.MemoryMB <= 0 {
		t.Fatalf("resources %+v", cr)
	}
	if cr.Tags["preset"] != "k3s" {
		t.Fatalf("tags %+v", cr.Tags)
	}
	has6443 := false
	for _, f := range cr.Forwards {
		if f.GuestPort == 6443 {
			has6443 = true
		}
	}
	if !has6443 {
		t.Fatalf("forwards %+v", cr.Forwards)
	}
	// already has 6443
	cr2, err := s.buildCreate(createIn{Preset: "k3s", Publish: []string{"6443"}})
	if err != nil {
		t.Fatal(err)
	}
	n := 0
	for _, f := range cr2.Forwards {
		if f.GuestPort == 6443 {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("dup 6443: %+v", cr2.Forwards)
	}
	// preset + userdata merge
	cr3, err := s.buildCreate(createIn{
		Preset:   "docker",
		Userdata: "#cloud-config\npackages:\n  - curl\n",
	})
	if err != nil {
		t.Fatal(err)
	}
	if cr3.Userdata == "" || !strings.Contains(cr3.Userdata, "curl") {
		t.Fatalf("merge userdata:\n%s", cr3.Userdata)
	}
	// bad preset
	if _, err := s.buildCreate(createIn{Preset: "nope-xyz"}); err == nil {
		t.Fatal("bad preset")
	}
	// defaults image/wait
	cr4, err := s.buildCreate(createIn{})
	if err != nil {
		t.Fatal(err)
	}
	if cr4.Image != DefaultCreateImage || cr4.Wait != DefaultCreateWait {
		t.Fatalf("%+v", cr4)
	}
}

func TestIsNotFoundAndHelpers(t *testing.T) {
	t.Parallel()
	if isNotFound(nil) || !isNotFound(errors.New("vm not found")) {
		t.Fatal("notfound")
	}
	if !isNotFound(errors.New("HTTP 404")) {
		t.Fatal("404")
	}
	if !isMostlyText(nil) || isMostlyText([]byte{0, 1}) {
		t.Fatal("text")
	}
	if truncateRunes("hi", 10) != "hi" {
		t.Fatal("trunc short")
	}
	if !strings.Contains(truncateRunes(strings.Repeat("a", 20), 5), "truncated") {
		t.Fatal("trunc")
	}
	if sanitizeName("!!!") != "sandbox" {
		t.Fatal(sanitizeName("!!!"))
	}
	if shellJoin([]string{"a b", "c"}) == "" {
		t.Fatal("shellJoin")
	}
}

func failingClient(t *testing.T, healthFail, infoFail, listNil bool, code int) *client.Client {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		if healthFail {
			http.Error(w, "down", 500)
			return
		}
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	mux.HandleFunc("/info", func(w http.ResponseWriter, r *http.Request) {
		if infoFail {
			http.Error(w, "no", 500)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]string{"name": "g"})
	})
	mux.HandleFunc("/vms", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			if listNil {
				// null JSON → client may return nil slice
				_, _ = w.Write([]byte("null"))
				return
			}
			if code >= 400 {
				http.Error(w, `{"error":"list fail"}`, code)
				return
			}
			_ = json.NewEncoder(w).Encode([]*client.Instance{})
			return
		}
		if r.Method == http.MethodPost {
			http.Error(w, `{"error":"create fail"}`, 500)
			return
		}
	})
	mux.HandleFunc("/vms/", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, 404)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestToolHandlersErrorPaths(t *testing.T) {
	ctx := context.Background()
	// health fail
	s := &Server{Client: failingClient(t, true, false, false, 200)}
	if _, _, err := s.toolHealth(ctx, nil, emptyIn{}); err == nil {
		t.Fatal("health")
	}
	// info fail
	s = &Server{Client: failingClient(t, false, true, false, 200)}
	if _, _, err := s.toolHealth(ctx, nil, emptyIn{}); err == nil {
		t.Fatal("info")
	}
	// list fail
	s = &Server{Client: failingClient(t, false, false, false, 500)}
	if _, _, err := s.toolListVMs(ctx, nil, emptyIn{}); err == nil {
		t.Fatal("list")
	}
	// list null → empty
	s = &Server{Client: failingClient(t, false, false, true, 200)}
	res, _, err := s.toolListVMs(ctx, nil, emptyIn{})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	// create fail
	s = &Server{Client: failingClient(t, false, false, false, 200)}
	if _, _, err := s.toolCreateVM(ctx, nil, createIn{Name: "x"}); err == nil {
		t.Fatal("create")
	}
	// start/stop/delete errors
	if _, _, err := s.toolStartVM(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("start")
	}
	if _, _, err := s.toolStopVM(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("stop")
	}
	// lifecycle errors
	if _, _, err := s.toolStatus(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("status")
	}
	if _, _, err := s.toolPauseVM(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("pause")
	}
	if _, _, err := s.toolResumeVM(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("resume")
	}
	if _, _, err := s.toolSuspendVM(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("suspend")
	}
	if _, _, err := s.toolRestoreVM(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("restore")
	}
	if _, _, err := s.toolSecretLS(ctx, nil, emptyIn{}); err == nil {
		t.Fatal("secret_ls")
	}
	// delete not found → ok missing
	out, _, err := s.toolDeleteVM(ctx, nil, nameIn{Name: "x"})
	if err != nil {
		t.Fatal(err)
	}
	_ = out
	// empty names
	if _, _, err := s.toolStartVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty start")
	}
	if _, _, err := s.toolStopVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty stop")
	}
	if _, _, err := s.toolDeleteVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty delete")
	}
	if _, _, err := s.toolStatus(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty status")
	}
	if _, _, err := s.toolPauseVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty pause")
	}
	if _, _, err := s.toolResumeVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty resume")
	}
	if _, _, err := s.toolSuspendVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty suspend")
	}
	if _, _, err := s.toolRestoreVM(ctx, nil, nameIn{}); err == nil {
		t.Fatal("empty restore")
	}
	if _, _, err := s.toolExec(ctx, nil, execIn{Name: "x"}); err == nil {
		t.Fatal("empty cmd")
	}
	if _, _, err := s.toolExec(ctx, nil, execIn{Name: "x", Cmd: "true", Timeout: "bad"}); err == nil {
		t.Fatal("bad timeout")
	}
	// write/read put errors
	if _, _, err := s.toolWriteFile(ctx, nil, writeFileIn{Name: "x", Path: "/a", Content: "b"}); err == nil {
		t.Fatal("write")
	}
	if _, _, err := s.toolReadFile(ctx, nil, readFileIn{Name: "x", Path: "/a"}); err == nil {
		t.Fatal("read")
	}
	// logs without host
	if _, _, err := s.toolLogs(ctx, nil, logsIn{Name: "x"}); err == nil {
		t.Fatal("logs host")
	}
	// image list/pull without host
	if _, _, err := s.toolImageList(ctx, nil, emptyIn{}); err == nil {
		t.Fatal("images host")
	}
	if _, _, err := s.toolImagePull(ctx, nil, imagePullIn{ID: "x"}); err == nil {
		t.Fatal("pull host")
	}
	// stats fail
	if _, _, err := s.toolStats(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("stats")
	}
	// agent health fail
	if _, _, err := s.toolAgentHealth(ctx, nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("agent")
	}
	// forward errors
	if _, _, err := s.toolForwardAdd(ctx, nil, forwardAddIn{Name: "x", GuestPort: 1}); err == nil {
		t.Fatal("fwd add")
	}
	if _, _, err := s.toolForwardRemove(ctx, nil, forwardRmIn{Name: "x", HostPort: 1}); err == nil {
		t.Fatal("fwd rm")
	}
	// fs ops
	if _, _, err := s.toolFSReadDir(ctx, nil, fsPathIn{Name: "x", Path: "/"}); err == nil {
		t.Fatal("readdir")
	}
	if _, _, err := s.toolFSStat(ctx, nil, fsPathIn{Name: "x", Path: "/"}); err == nil {
		t.Fatal("stat")
	}
	if _, _, err := s.toolFSMkdir(ctx, nil, fsPathIn{Name: "x", Path: "/a"}); err == nil {
		t.Fatal("mkdir")
	}
	if _, _, err := s.toolFSRemove(ctx, nil, fsPathIn{Name: "x", Path: "/a"}); err == nil {
		t.Fatal("remove")
	}
	// put/get tar
	if _, _, err := s.toolPutTar(ctx, nil, tarIn{Name: "x", Path: "/a", Base64: "YQ=="}); err == nil {
		t.Fatal("puttar")
	}
	if _, _, err := s.toolGetTar(ctx, nil, tarIn{Name: "x", Path: "/a"}); err == nil {
		t.Fatal("gettar")
	}
	_ = io.Discard
	_ = mcp.Implementation{}
}

func TestToolExecBufferedAndStreamFallback(t *testing.T) {
	// Stream fails (404), buffered also fails → error
	s := &Server{Client: failingClient(t, false, false, false, 200)}
	stream := true
	if _, _, err := s.toolExec(context.Background(), nil, execIn{Name: "x", Cmd: "true", Stream: &stream}); err == nil {
		t.Fatal("stream fail")
	}
	// buffered false path with 404
	stream = false
	if _, _, err := s.toolExec(context.Background(), nil, execIn{Name: "x", Cmd: "true", Stream: &stream}); err == nil {
		t.Fatal("buffered fail")
	}
}

func TestToolDeleteNon404Error(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/vms/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			http.Error(w, `{"error":"busy"}`, http.StatusConflict)
			return
		}
		http.NotFound(w, r)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	s := &Server{Client: c}
	if _, _, err := s.toolDeleteVM(context.Background(), nil, nameIn{Name: "x"}); err == nil {
		t.Fatal("expected delete error")
	}
}
