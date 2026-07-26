package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/cxdy/grain/api"
	grainapi "github.com/cxdy/grain/internal/api"
)

func TestAuthMiddlewareNoTokenOpen(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	h := s.Handler()

	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/info", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("info without auth config: %d %s", rr.Code, rr.Body.String())
	}
}

func TestAuthMiddlewareRequiresBearer(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.APIToken = "test-secret"
	h := s.Handler()

	// healthz always open
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("healthz: %d", rr.Code)
	}

	// no header → 401
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/info", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 without token, got %d %s", rr.Code, rr.Body.String())
	}
	var errBody map[string]string
	if err := json.Unmarshal(rr.Body.Bytes(), &errBody); err != nil {
		t.Fatal(err)
	}
	if errBody["error"] != "unauthorized" {
		t.Fatalf("body %+v", errBody)
	}

	// wrong token → 401
	rr = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/info", nil)
	req.Header.Set("Authorization", "Bearer wrong")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("want 401 wrong token, got %d", rr.Code)
	}

	// correct token → 200
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/info", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("want 200 with token, got %d %s", rr.Code, rr.Body.String())
	}

	// list with token
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/vms", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("list: %d %s", rr.Code, rr.Body.String())
	}

	// openapi also protected when token set
	rr = httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil))
	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("openapi without token: %d", rr.Code)
	}
	rr = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodGet, "/openapi.yaml", nil)
	req.Header.Set("Authorization", "Bearer test-secret")
	h.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("openapi with token: %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "openapi:") {
		t.Fatalf("openapi body missing openapi key")
	}
	if ct := rr.Header().Get("Content-Type"); !strings.Contains(ct, "yaml") {
		t.Fatalf("content-type %q", ct)
	}
}

func TestOpenAPIEmbedded(t *testing.T) {
	t.Parallel()
	if len(api.OpenAPIYAML) == 0 {
		t.Fatal("empty embedded OpenAPI")
	}
	if !strings.Contains(string(api.OpenAPIYAML), "Grain Daemon API") {
		t.Fatal("unexpected openapi content")
	}

	s := testServer(t)
	h := s.Handler()
	for _, path := range []string{"/openapi.yaml", "/openapi.json"} {
		rr := httptest.NewRecorder()
		h.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, path, nil))
		if rr.Code != http.StatusOK {
			t.Fatalf("%s: %d", path, rr.Code)
		}
		if rr.Body.Len() != len(api.OpenAPIYAML) {
			t.Fatalf("%s length %d want %d", path, rr.Body.Len(), len(api.OpenAPIYAML))
		}
	}
}

func TestAPIClientSendsBearerToken(t *testing.T) {
	t.Parallel()
	s := testServer(t)
	s.APIToken = "cli-token"
	ts := httptest.NewServer(s.Handler())
	t.Cleanup(ts.Close)

	ctx := t.Context()

	// Without token → unauthorized
	c := &grainapi.Client{Base: ts.URL, HTTP: ts.Client()}
	if _, err := c.List(ctx); err == nil {
		t.Fatal("expected List error without token")
	}

	// With token → create/get/delete OK
	c2 := &grainapi.Client{Base: ts.URL, HTTP: ts.Client(), Token: "cli-token"}
	inst, err := c2.Create(ctx, grainapi.CreateRequest{Name: "tok1", Persistent: false})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := c2.Get(ctx, inst.Name)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "tok1" {
		t.Fatalf("name %q", got.Name)
	}
	if err := c2.Delete(ctx, "tok1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}
