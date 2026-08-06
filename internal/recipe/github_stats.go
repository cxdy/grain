package recipe

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// githubAPIBase is the GitHub API origin (overridden in tests).
var githubAPIBase = "https://api.github.com"

// ReleaseAssetDownload is one GitHub Release asset download_count.
type ReleaseAssetDownload struct {
	Name          string `json:"name"`
	DownloadCount int    `json:"download_count"`
	URL           string `json:"browser_download_url,omitempty"`
}

// FetchReleaseAssetDownloads lists assets for a GitHub release tag via the public API.
// repo is "owner/name" (e.g. cxdy/grain). tag is a release tag (e.g. golden-latest, v0.7.0).
// Uses download_count only — no extra infra (see product notes).
func FetchReleaseAssetDownloads(client *http.Client, repo, tag string) ([]ReleaseAssetDownload, error) {
	repo = strings.TrimSpace(repo)
	tag = strings.TrimSpace(tag)
	if repo == "" || tag == "" {
		return nil, fmt.Errorf("repo and tag are required")
	}
	if client == nil {
		client = &http.Client{Timeout: 15 * time.Second}
	}
	url := fmt.Sprintf("%s/repos/%s/releases/tags/%s", strings.TrimRight(githubAPIBase, "/"), repo, tag)
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "grain-recipe-stats")
	res, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("release tag %q not found", tag)
	}
	if res.StatusCode >= 300 {
		return nil, fmt.Errorf("github releases: HTTP %d", res.StatusCode)
	}
	var body struct {
		Assets []struct {
			Name               string `json:"name"`
			DownloadCount      int    `json:"download_count"`
			BrowserDownloadURL string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return nil, err
	}
	out := make([]ReleaseAssetDownload, 0, len(body.Assets))
	for _, a := range body.Assets {
		out = append(out, ReleaseAssetDownload{
			Name:          a.Name,
			DownloadCount: a.DownloadCount,
			URL:           a.BrowserDownloadURL,
		})
	}
	return out, nil
}

// SumDownloadCounts totals download_count across assets (optional name prefix filter).
func SumDownloadCounts(assets []ReleaseAssetDownload, namePrefix string) int {
	n := 0
	pref := strings.TrimSpace(namePrefix)
	for _, a := range assets {
		if pref != "" && !strings.HasPrefix(a.Name, pref) {
			continue
		}
		n += a.DownloadCount
	}
	return n
}
