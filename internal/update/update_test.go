package update_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/cxdy/grain/internal/update"
)

func TestNormalizeAndCompare(t *testing.T) {
	t.Parallel()
	if update.NormalizeVersion("v0.2.2") != "0.2.2" {
		t.Fatal(update.NormalizeVersion("v0.2.2"))
	}
	if update.Compare("0.2.1", "v0.2.2") >= 0 {
		t.Fatal("0.2.1 should be < 0.2.2")
	}
	if update.Compare("0.2.2", "0.2.2") != 0 {
		t.Fatal("equal")
	}
	if update.Compare("0.3.0", "0.2.9") <= 0 {
		t.Fatal("0.3.0 > 0.2.9")
	}
	if update.Compare("dev", "0.2.2") != 0 {
		t.Fatal("dev not comparable")
	}
	if update.Comparable("dev") {
		t.Fatal("dev not comparable")
	}
	if !update.Comparable("v1.0.0") {
		t.Fatal("v1.0.0 comparable")
	}
}

func TestCheckLiveAndCache(t *testing.T) {
	t.Parallel()
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v0.9.9",
			"html_url": "https://github.com/cxdy/grain/releases/tag/v0.9.9",
		})
	}))
	t.Cleanup(srv.Close)

	dir := t.TempDir()
	res, err := update.Check(update.Options{
		Current:    "0.2.2",
		DataDir:    dir,
		APIURL:     srv.URL,
		HTTPClient: srv.Client(),
		CacheTTL:   time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.UpdateAvail || res.Latest != "v0.9.9" || res.FromCache {
		t.Fatalf("%+v", res)
	}
	if hits != 1 {
		t.Fatalf("hits %d", hits)
	}

	// Second call should use cache.
	res2, err := update.Check(update.Options{
		Current:    "0.2.2",
		DataDir:    dir,
		APIURL:     srv.URL,
		HTTPClient: srv.Client(),
		CacheTTL:   time.Hour,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res2.FromCache || hits != 1 {
		t.Fatalf("cache miss: %+v hits=%d", res2, hits)
	}

	// Force refresh.
	_, err = update.Check(update.Options{
		Current:      "0.2.2",
		DataDir:      dir,
		APIURL:       srv.URL,
		HTTPClient:   srv.Client(),
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 {
		t.Fatalf("force hits %d", hits)
	}

	// CacheOnly
	res3, err := update.Check(update.Options{
		Current:   "0.9.9",
		DataDir:   dir,
		CacheOnly: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res3.UpdateAvail {
		t.Fatalf("same version should not need update: %+v", res3)
	}
}

func TestCheckUpToDate(t *testing.T) {
	t.Parallel()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v0.2.2"})
	}))
	t.Cleanup(srv.Close)
	res, err := update.Check(update.Options{
		Current:      "v0.2.2",
		DataDir:      t.TempDir(),
		APIURL:       srv.URL,
		HTTPClient:   srv.Client(),
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.UpdateAvail {
		t.Fatalf("%+v", res)
	}
}

func TestCheckNetworkErrorFallsBackToCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := update.CachePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	body, _ := json.Marshal(update.Cache{
		Latest:    "v1.2.3",
		CheckedAt: time.Now().Add(-48 * time.Hour),
	})
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatal(err)
	}

	// Dead server / bad URL after forcing refresh due to stale cache.
	res, err := update.Check(update.Options{
		Current:    "1.0.0",
		DataDir:    dir,
		APIURL:     "http://127.0.0.1:1", // connection refused
		HTTPClient: &http.Client{Timeout: 100 * time.Millisecond},
		CacheTTL:   time.Minute, // stale → try network → fall back
	})
	if err != nil {
		t.Fatal(err)
	}
	if !res.FromCache || res.Latest != "v1.2.3" || !res.UpdateAvail {
		t.Fatalf("%+v", res)
	}
}

func TestCachePath(t *testing.T) {
	t.Parallel()
	if update.CachePath("") != "" {
		t.Fatal("empty")
	}
	p := update.CachePath("/tmp/grain")
	if filepath.Base(filepath.Dir(p)) != "cache" {
		t.Fatal(p)
	}
}

func TestComparableEdges(t *testing.T) {
	t.Parallel()
	if !update.Comparable("1.2.3-rc.1") {
		t.Fatal("prerelease core still comparable")
	}
	if !update.Comparable("1.2.3+build") {
		t.Fatal("build meta")
	}
	if update.Comparable("1") {
		t.Fatal("single segment")
	}
	if update.Comparable("1.2.3.4.5") {
		t.Fatal("too many segments")
	}
	if update.Comparable("1..2") {
		t.Fatal("empty part")
	}
	if update.Comparable("1.2.x") {
		t.Fatal("non-numeric")
	}
	if update.Comparable("") || update.Comparable("  dirty  ") {
		t.Fatal("empty/dirty")
	}
	// Compare pads shorter version parts
	if update.Compare("1.2", "1.2.0") != 0 {
		t.Fatal("pad equal")
	}
	if update.Compare("1.2.1", "1.2") <= 0 {
		t.Fatal("1.2.1 > 1.2")
	}
	if update.Compare("2.0", "1.9.9") <= 0 {
		t.Fatal("2.0 > 1.9.9")
	}
}

func TestCheckCacheOnlyMissing(t *testing.T) {
	t.Parallel()
	_, err := update.Check(update.Options{
		Current:   "1.0.0",
		DataDir:   t.TempDir(),
		CacheOnly: true,
	})
	if err == nil {
		t.Fatal("expected no cache error")
	}
}

func TestCheckNetworkErrorNoCache(t *testing.T) {
	t.Parallel()
	_, err := update.Check(update.Options{
		Current:      "1.0.0",
		DataDir:      t.TempDir(),
		APIURL:       "http://127.0.0.1:1",
		HTTPClient:   &http.Client{Timeout: 50 * time.Millisecond},
		ForceRefresh: true,
	})
	if err == nil {
		t.Fatal("expected network error")
	}
}

func TestFetchLatestDefaultsAndErrors(t *testing.T) {
	t.Parallel()
	// HTTP error status
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte("nope"))
	}))
	t.Cleanup(srv.Close)
	_, err := update.Check(update.Options{
		Current:      "0.1.0",
		DataDir:      t.TempDir(),
		APIURL:       srv.URL,
		HTTPClient:   srv.Client(),
		ForceRefresh: true,
	})
	if err == nil {
		t.Fatal("expected HTTP status error")
	}

	// bad JSON
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not-json"))
	}))
	t.Cleanup(srv2.Close)
	_, err = update.Check(update.Options{
		Current:      "0.1.0",
		DataDir:      t.TempDir(),
		APIURL:       srv2.URL,
		HTTPClient:   srv2.Client(),
		ForceRefresh: true,
	})
	if err == nil {
		t.Fatal("expected parse error")
	}

	// empty tag_name
	srv3 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "  ", "html_url": "x"})
	}))
	t.Cleanup(srv3.Close)
	_, err = update.Check(update.Options{
		Current:      "0.1.0",
		DataDir:      t.TempDir(),
		APIURL:       srv3.URL,
		HTTPClient:   srv3.Client(),
		ForceRefresh: true,
	})
	if err == nil {
		t.Fatal("expected empty tag error")
	}
}

func TestCheckDefaultClientAndCorruptCache(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// corrupt cache ignored; live fetch with real client + custom API
	path := update.CachePath(dir)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{bad"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{
			"tag_name": "v9.9.9",
			"html_url": "https://example.com/r",
		})
	}))
	t.Cleanup(srv.Close)
	// nil HTTPClient → uses DefaultHTTPTimeout client
	res, err := update.Check(update.Options{
		Current:      "1.0.0",
		DataDir:      dir,
		APIURL:       srv.URL,
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Latest != "v9.9.9" || !res.UpdateAvail {
		t.Fatalf("%+v", res)
	}
}

func TestWriteCacheMkdirFail(t *testing.T) {
	t.Parallel()
	// DataDir is a file → cache dir cannot be created
	base := t.TempDir()
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]string{"tag_name": "v1.0.0"})
	}))
	t.Cleanup(srv.Close)
	// writeCache failure is soft-ignored; Check still succeeds
	res, err := update.Check(update.Options{
		Current:      "1.0.0",
		DataDir:      file,
		APIURL:       srv.URL,
		HTTPClient:   srv.Client(),
		ForceRefresh: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Latest != "v1.0.0" {
		t.Fatalf("%+v", res)
	}
}
