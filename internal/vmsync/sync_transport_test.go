package vmsync

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
)

// fakeFSServer implements a minimal agent/API-shaped HTTP surface for syncFS tests.
func fakeFSServer(t *testing.T, store map[string][]byte, meta map[string]agent.FSInfo) *httptest.Server {
	t.Helper()
	if store == nil {
		store = map[string][]byte{}
	}
	if meta == nil {
		meta = map[string]agent.FSInfo{}
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Normalize path: strip optional /vms/{name} prefix for API client.
		p := r.URL.Path
		if i := strings.Index(p, "/fs/"); i >= 0 {
			p = p[i:]
		} else if i := strings.Index(p, "/cp"); i >= 0 {
			p = p[i:]
		}
		switch {
		case r.Method == http.MethodGet && (p == "/fs/stat" || strings.HasPrefix(p, "/fs/stat")):
			path := r.URL.Query().Get("path")
			info, ok := meta[path]
			if !ok {
				if b, ok := store[path]; ok {
					info = agent.FSInfo{Name: path, Type: "file", Size: int64(len(b)), Mode: "0644"}
				} else {
					http.NotFound(w, r)
					return
				}
			}
			_ = json.NewEncoder(w).Encode(info)
		case r.Method == http.MethodGet && strings.HasPrefix(p, "/fs/readdir"):
			path := r.URL.Query().Get("path")
			var out []agent.FSInfo
			prefix := strings.TrimSuffix(path, "/") + "/"
			if path == "/" {
				prefix = "/"
			}
			seen := map[string]bool{}
			for k, b := range store {
				if path != "/" && !strings.HasPrefix(k, prefix) && k != path {
					continue
				}
				name := strings.TrimPrefix(k, prefix)
				if path == "/" {
					name = strings.TrimPrefix(k, "/")
				}
				if name == "" || strings.Contains(name, "/") {
					if i := strings.Index(name, "/"); i >= 0 {
						dir := name[:i]
						if !seen[dir] {
							seen[dir] = true
							out = append(out, agent.FSInfo{Name: dir, Type: "directory", Mode: "0755"})
						}
					}
					continue
				}
				out = append(out, agent.FSInfo{Name: name, Type: "file", Size: int64(len(b)), Mode: "0644"})
			}
			_ = json.NewEncoder(w).Encode(out)
		case r.Method == http.MethodPost && strings.HasPrefix(p, "/fs/mkdir"):
			var req agent.MkdirRequest
			_ = json.NewDecoder(r.Body).Decode(&req)
			meta[req.Path] = agent.FSInfo{Name: req.Path, Type: "directory", Mode: "0755"}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodDelete && strings.HasPrefix(p, "/fs/remove"):
			path := r.URL.Query().Get("path")
			delete(store, path)
			delete(meta, path)
			w.WriteHeader(http.StatusNoContent)
		case (r.Method == http.MethodPut || r.Method == http.MethodPost || r.Method == http.MethodGet) && (p == "/cp" || strings.HasPrefix(p, "/cp")):
			path := r.URL.Query().Get("path")
			if r.Method == http.MethodPut || r.Method == http.MethodPost {
				b, _ := io.ReadAll(r.Body)
				store[path] = b
				meta[path] = agent.FSInfo{Name: path, Type: "file", Size: int64(len(b)), Mode: "0644", Mtime: 42}
				w.WriteHeader(http.StatusNoContent)
				return
			}
			b, ok := store[path]
			if !ok {
				http.NotFound(w, r)
				return
			}
			_, _ = w.Write(b)
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestAPISyncFSBindVMName(t *testing.T) {
	store := map[string][]byte{}
	meta := map[string]agent.FSInfo{}
	srv := fakeFSServer(t, store, meta)
	defer srv.Close()

	// API client expects /vms/{name}/... routes — wrap base.
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		// Accept only lab VM prefix
		if !strings.HasPrefix(r.URL.Path, "/vms/lab/") {
			http.Error(w, "wrong vm", http.StatusNotFound)
			return
		}
		// Rewrite to agent-shaped path for fake server logic by reusing handler.
		r2 := r.Clone(r.Context())
		r2.URL.Path = strings.TrimPrefix(r.URL.Path, "/vms/lab")
		srv.Config.Handler.ServeHTTP(w, r2)
	})
	apiSrv := httptest.NewServer(mux)
	defer apiSrv.Close()

	c := &api.Client{Base: apiSrv.URL, HTTP: apiSrv.Client()}
	fs := newAPISyncFS(c, "lab")
	ctx := context.Background()

	body := []byte("hello-api")
	if err := fs.PutFile(ctx, "/work/a.txt", bytes.NewReader(body), int64(len(body)), agent.CPOpts{Mode: "0644"}); err != nil {
		t.Fatalf("put: %v", err)
	}
	info, err := fs.Stat(ctx, "/work/a.txt")
	if err != nil || info.Size != int64(len(body)) {
		t.Fatalf("stat: %+v %v", info, err)
	}
	var buf bytes.Buffer
	if err := fs.GetFile(ctx, "/work/a.txt", &buf); err != nil || buf.String() != "hello-api" {
		t.Fatalf("get: %q %v", buf.String(), err)
	}
	if err := fs.Mkdir(ctx, "/work/d", true, "0755"); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := fs.Remove(ctx, "/work/a.txt", false); err != nil {
		t.Fatalf("remove: %v", err)
	}
}

func TestAgentSyncFS(t *testing.T) {
	store := map[string][]byte{"/g/x": []byte("v")}
	meta := map[string]agent.FSInfo{
		"/g/x": {Name: "x", Type: "file", Size: 1, Mode: "0644", Mtime: 1},
	}
	srv := fakeFSServer(t, store, meta)
	defer srv.Close()

	ac := &agent.Client{BaseURL: srv.URL, HTTP: srv.Client()}
	fs := newAgentSyncFS(ac)
	ctx := context.Background()

	info, err := fs.Stat(ctx, "/g/x")
	if err != nil || info.Size != 1 {
		t.Fatalf("stat: %+v %v", info, err)
	}
	entries, err := fs.ReadDir(ctx, "/g")
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected entries")
	}
	var buf bytes.Buffer
	if err := fs.GetFile(ctx, "/g/x", &buf); err != nil || buf.String() != "v" {
		t.Fatalf("get %q %v", buf.String(), err)
	}
	b := []byte("new")
	if err := fs.PutFile(ctx, "/g/y", bytes.NewReader(b), 3, agent.CPOpts{}); err != nil {
		t.Fatal(err)
	}
}
