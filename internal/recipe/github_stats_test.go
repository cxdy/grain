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
}
