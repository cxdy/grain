package recipe

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchReleaseAssetDownloads(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/repos/cxdy/grain/releases/tags/golden-latest") {
			http.NotFound(w, r)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"assets": []map[string]interface{}{
				{"name": "grain-ubuntu-arm64.qcow2", "download_count": 12, "browser_download_url": "https://example/a"},
				{"name": "grain-ubuntu-amd64.qcow2", "download_count": 8, "browser_download_url": "https://example/b"},
				{"name": "other.txt", "download_count": 1},
			},
		})
	}))
	t.Cleanup(srv.Close)

	prev := githubAPIBase
	githubAPIBase = srv.URL
	t.Cleanup(func() { githubAPIBase = prev })

	assets, err := FetchReleaseAssetDownloads(srv.Client(), "cxdy/grain", "golden-latest")
	if err != nil {
		t.Fatal(err)
	}
	if len(assets) != 3 {
		t.Fatalf("%+v", assets)
	}
	if SumDownloadCounts(assets, "grain-ubuntu") != 20 {
		t.Fatalf("sum=%d", SumDownloadCounts(assets, "grain-ubuntu"))
	}
	if SumDownloadCounts(assets, "") != 21 {
		t.Fatalf("all=%d", SumDownloadCounts(assets, ""))
	}
}

func TestFetchReleaseAssetDownloadsErrors(t *testing.T) {
	t.Parallel()
	if _, err := FetchReleaseAssetDownloads(nil, "", "v1"); err == nil {
		t.Fatal("want error")
	}
	if _, err := FetchReleaseAssetDownloads(nil, "owner/repo", ""); err == nil {
		t.Fatal("want empty tag error")
	}
}

func TestFetchReleaseAssetDownloadsHTTPErrors(t *testing.T) {
	// 404
	srv404 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.NotFound(w, r)
	}))
	t.Cleanup(srv404.Close)
	prev := githubAPIBase
	githubAPIBase = srv404.URL
	t.Cleanup(func() { githubAPIBase = prev })

	if _, err := FetchReleaseAssetDownloads(srv404.Client(), "o/r", "nope"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("404: %v", err)
	}

	// 500
	srv500 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	}))
	t.Cleanup(srv500.Close)
	githubAPIBase = srv500.URL
	if _, err := FetchReleaseAssetDownloads(srv500.Client(), "o/r", "v1"); err == nil || !strings.Contains(err.Error(), "HTTP 500") {
		t.Fatalf("500: %v", err)
	}

	// invalid JSON
	srvBad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("{not-json"))
	}))
	t.Cleanup(srvBad.Close)
	githubAPIBase = srvBad.URL
	if _, err := FetchReleaseAssetDownloads(srvBad.Client(), "o/r", "v1"); err == nil {
		t.Fatal("bad json")
	}

	// empty assets ok
	srvEmpty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"assets": []interface{}{}})
	}))
	t.Cleanup(srvEmpty.Close)
	githubAPIBase = srvEmpty.URL
	assets, err := FetchReleaseAssetDownloads(srvEmpty.Client(), "o/r", "v1")
	if err != nil || len(assets) != 0 {
		t.Fatalf("%+v %v", assets, err)
	}
	// nil client uses default — still works against httptest if we set base
	// (default client can call localhost)
	assets, err = FetchReleaseAssetDownloads(nil, "o/r", "v1")
	if err != nil || len(assets) != 0 {
		t.Fatalf("nil client: %+v %v", assets, err)
	}
}

func TestSumDownloadCountsPrefix(t *testing.T) {
	t.Parallel()
	assets := []ReleaseAssetDownload{
		{Name: "a.bin", DownloadCount: 5},
		{Name: "b.bin", DownloadCount: 3},
	}
	if SumDownloadCounts(assets, "z") != 0 {
		t.Fatal("no match")
	}
	if SumDownloadCounts(nil, "") != 0 {
		t.Fatal("nil")
	}
}
