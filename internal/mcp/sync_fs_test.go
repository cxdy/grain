package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/agent"
)

// clientSyncFS uses PUT for PutFile (client.PutFile), not POST.
func TestClientSyncFSAllMethods(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") == "" {
			http.Error(w, "path required", 400)
			return
		}
		_ = json.NewEncoder(w).Encode(client.FSInfo{
			Name: "a.txt", Type: "file", Size: 3, Mtime: 1, Mode: "0644",
		})
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]client.FSInfo{
			{Name: "a.txt", Type: "file", Size: 3, Mtime: 1, Mode: "0644"},
			{Name: "d", Type: "directory", Size: 0, Mtime: 1, Mode: "0755"},
		})
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if string(body) != "hi!" {
			t.Errorf("put body %q", body)
		}
		w.WriteHeader(http.StatusNoContent)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("hi"))
	})

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	fs := newClientSyncFS(c, "lab")
	ctx := context.Background()

	st, err := fs.Stat(ctx, "/tmp/a.txt")
	if err != nil || st == nil || st.Name != "a.txt" || st.Size != 3 || st.Mode != "0644" {
		t.Fatalf("Stat: %+v %v", st, err)
	}
	ents, err := fs.ReadDir(ctx, "/tmp")
	if err != nil || len(ents) != 2 {
		t.Fatalf("ReadDir: %v %v", ents, err)
	}
	if ents[0].Name != "a.txt" || ents[1].Type != "directory" {
		t.Fatalf("entries %+v", ents)
	}
	if err := fs.Mkdir(ctx, "/tmp/n", true, "0755"); err != nil {
		t.Fatal(err)
	}
	if err := fs.Remove(ctx, "/tmp/n", true); err != nil {
		t.Fatal(err)
	}
	uid, gid := uint32(1000), uint32(1000)
	if err := fs.PutFile(ctx, "/tmp/a.txt", strings.NewReader("hi!"), 3, agent.CPOpts{
		UID: &uid, GID: &gid, Mode: "0644",
	}); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	if err := fs.GetFile(ctx, "/tmp/a.txt", &buf); err != nil || buf.String() != "hi" {
		t.Fatalf("GetFile %q %v", buf.String(), err)
	}
}

func TestClientSyncFSErrorsAndNilInfo(t *testing.T) {
	t.Parallel()
	mux := http.NewServeMux()
	mux.HandleFunc("GET /vms/{name}/fs/stat", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
	})
	mux.HandleFunc("GET /vms/{name}/fs/readdir", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":"bad"}`, http.StatusBadRequest)
	})
	mux.HandleFunc("POST /vms/{name}/fs/mkdir", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	mux.HandleFunc("DELETE /vms/{name}/fs/remove", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	mux.HandleFunc("PUT /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	mux.HandleFunc("GET /vms/{name}/cp", func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "fail", 500)
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	c, err := client.DialHTTP(srv.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	fs := newClientSyncFS(c, "x")
	ctx := context.Background()
	if _, err := fs.Stat(ctx, "/missing"); err == nil {
		t.Fatal("expected stat error")
	}
	if _, err := fs.ReadDir(ctx, "/missing"); err == nil {
		t.Fatal("expected readdir error")
	}
	if err := fs.Mkdir(ctx, "/a", true, "0755"); err == nil {
		t.Fatal("expected mkdir error")
	}
	if err := fs.Remove(ctx, "/a", true); err == nil {
		t.Fatal("expected remove error")
	}
	if err := fs.PutFile(ctx, "/f", strings.NewReader("x"), 1, agent.CPOpts{}); err == nil {
		t.Fatal("expected put error")
	}
	if err := fs.GetFile(ctx, "/f", io.Discard); err == nil {
		t.Fatal("expected get error")
	}
	if got := clientFSInfoToAgent(nil); got != nil {
		t.Fatal("nil info")
	}
	// non-nil conversion
	got := clientFSInfoToAgent(&client.FSInfo{Name: "n", Type: "file", Size: 9, Mtime: 2, Mode: "0600"})
	if got == nil || got.Name != "n" || got.Size != 9 || got.Mode != "0600" {
		t.Fatalf("%+v", got)
	}
}
