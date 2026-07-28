// Package update checks GitHub Releases for newer grain CLI versions.
package update

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

const (
	// DefaultAPI is the GitHub Releases latest endpoint for cxdy/grain.
	DefaultAPI = "https://api.github.com/repos/cxdy/grain/releases/latest"
	// DefaultInstallScript is the curl|bash installer used by grain update.
	DefaultInstallScript = "https://raw.githubusercontent.com/cxdy/grain/main/scripts/install.sh"
	// DefaultCacheTTL is how often background/CLI notices refresh from the network.
	DefaultCacheTTL = 24 * time.Hour
	// DefaultHTTPTimeout bounds network calls so CLI stays snappy.
	DefaultHTTPTimeout = 3 * time.Second
)

// Result is a version comparison against the latest published release.
type Result struct {
	Current     string
	Latest      string
	HTMLURL     string
	UpdateAvail bool
	// FromCache is true when Latest came from the on-disk cache (may be stale).
	FromCache bool
	// CheckedAt is when Latest was fetched (cache or live).
	CheckedAt time.Time
}

// Cache holds the last successful remote lookup.
type Cache struct {
	Latest    string    `json:"latest"`
	HTMLURL   string    `json:"html_url,omitempty"`
	CheckedAt time.Time `json:"checked_at"`
}

// Options control Fetch / Check behavior.
type Options struct {
	// Current is the running binary version (e.g. "0.2.2", "v0.2.2", "dev").
	Current string
	// DataDir is ~/.grain (cache lives under DataDir/cache/update-check.json).
	DataDir string
	// APIURL overrides DefaultAPI (tests).
	APIURL string
	// HTTPClient overrides the default short-timeout client (tests).
	HTTPClient *http.Client
	// CacheTTL is max age before a live refresh is attempted (default 24h).
	CacheTTL time.Duration
	// ForceRefresh ignores a fresh cache and hits the network.
	ForceRefresh bool
	// CacheOnly never hits the network (notices path when offline is fine).
	CacheOnly bool
}

// CachePath returns the update-check cache file under dataDir.
func CachePath(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, "cache", "update-check.json")
}

// NormalizeVersion strips a leading "v" and trims space.
func NormalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// Comparable reports whether v looks like major.minor.patch (optional pre-release suffix ignored for ordering base).
func Comparable(v string) bool {
	v = NormalizeVersion(v)
	if v == "" || v == "dev" || strings.Contains(v, "dirty") {
		return false
	}
	// Accept 0.2.2, 0.2.2-rc.1, etc. — compare numeric core only.
	core := v
	if i := strings.IndexAny(core, "-+"); i >= 0 {
		core = core[:i]
	}
	parts := strings.Split(core, ".")
	if len(parts) < 2 || len(parts) > 4 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		if _, err := strconv.Atoi(p); err != nil {
			return false
		}
	}
	return true
}

// Compare returns -1 if a < b, 0 if equal, 1 if a > b.
// Non-comparable versions return 0 (treat as equal / no update signal).
func Compare(a, b string) int {
	if !Comparable(a) || !Comparable(b) {
		return 0
	}
	ap := versionParts(a)
	bp := versionParts(b)
	n := len(ap)
	if len(bp) > n {
		n = len(bp)
	}
	for i := 0; i < n; i++ {
		var av, bv int
		if i < len(ap) {
			av = ap[i]
		}
		if i < len(bp) {
			bv = bp[i]
		}
		if av < bv {
			return -1
		}
		if av > bv {
			return 1
		}
	}
	return 0
}

func versionParts(v string) []int {
	v = NormalizeVersion(v)
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	segs := strings.Split(v, ".")
	out := make([]int, 0, len(segs))
	for _, s := range segs {
		n, _ := strconv.Atoi(s)
		out = append(out, n)
	}
	return out
}

// Check returns whether current is behind latest, using cache and/or network.
func Check(opts Options) (Result, error) {
	cur := strings.TrimSpace(opts.Current)
	res := Result{Current: cur}

	ttl := opts.CacheTTL
	if ttl <= 0 {
		ttl = DefaultCacheTTL
	}

	cachePath := CachePath(opts.DataDir)
	var cached Cache
	haveCache := false
	if cachePath != "" {
		if c, err := readCache(cachePath); err == nil && c.Latest != "" {
			cached = c
			haveCache = true
		}
	}

	useCache := haveCache && !opts.ForceRefresh && time.Since(cached.CheckedAt) < ttl
	if opts.CacheOnly {
		if !haveCache {
			return res, fmt.Errorf("no update cache")
		}
		useCache = true
	}

	if useCache {
		res.Latest = cached.Latest
		res.HTMLURL = cached.HTMLURL
		res.CheckedAt = cached.CheckedAt
		res.FromCache = true
		res.UpdateAvail = Compare(cur, res.Latest) < 0
		return res, nil
	}

	latest, htmlURL, err := fetchLatest(opts)
	if err != nil {
		// Soft fallback: stale cache better than nothing for notices.
		if haveCache {
			res.Latest = cached.Latest
			res.HTMLURL = cached.HTMLURL
			res.CheckedAt = cached.CheckedAt
			res.FromCache = true
			res.UpdateAvail = Compare(cur, res.Latest) < 0
			return res, nil
		}
		return res, err
	}

	res.Latest = latest
	res.HTMLURL = htmlURL
	res.CheckedAt = time.Now().UTC()
	res.UpdateAvail = Compare(cur, res.Latest) < 0

	if cachePath != "" {
		_ = writeCache(cachePath, Cache{
			Latest:    res.Latest,
			HTMLURL:   res.HTMLURL,
			CheckedAt: res.CheckedAt,
		})
	}
	return res, nil
}

type githubRelease struct {
	TagName string `json:"tag_name"`
	HTMLURL string `json:"html_url"`
}

func fetchLatest(opts Options) (tag, htmlURL string, err error) {
	api := opts.APIURL
	if api == "" {
		api = DefaultAPI
	}
	client := opts.HTTPClient
	if client == nil {
		client = &http.Client{Timeout: DefaultHTTPTimeout}
	}
	req, err := http.NewRequest(http.MethodGet, api, nil)
	if err != nil {
		return "", "", err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "grain-cli-update-check")

	resp, err := client.Do(req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", "", fmt.Errorf("github releases: HTTP %d", resp.StatusCode)
	}
	var rel githubRelease
	if err := json.Unmarshal(body, &rel); err != nil {
		return "", "", fmt.Errorf("parse release json: %w", err)
	}
	tag = strings.TrimSpace(rel.TagName)
	if tag == "" {
		return "", "", fmt.Errorf("release has empty tag_name")
	}
	return tag, strings.TrimSpace(rel.HTMLURL), nil
}

func readCache(path string) (Cache, error) {
	var c Cache
	b, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	if err := json.Unmarshal(b, &c); err != nil {
		return c, err
	}
	return c, nil
}

func writeCache(path string, c Cache) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}
