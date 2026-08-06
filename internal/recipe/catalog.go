package recipe

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// CatalogAPIVersion for the official index document.
const CatalogAPIVersion = "grain.recipes/v1"

// DefaultCatalogURL is the official index (overridable via GRAIN_RECIPE_CATALOG_URL).
// Points at the monorepo recipes/catalog.json on main; tests inject httptest URLs.
const DefaultCatalogURL = "https://raw.githubusercontent.com/cxdy/grain/main/recipes/catalog.json"

// Catalog is the remote/official recipe index (bodies fetched opt-in).
type Catalog struct {
	APIVersion string         `json:"apiVersion"`
	Recipes    []CatalogEntry `json:"recipes"`
	// BaseURL is optional prefix for relative paths (set by client after fetch).
	BaseURL string `json:"-"`
}

// CatalogEntry is one index row.
type CatalogEntry struct {
	ID          string   `json:"id"`
	Title       string   `json:"title,omitempty"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Path        string   `json:"path,omitempty"` // relative to catalog or absolute URL
	URL         string   `json:"url,omitempty"`  // full URL override
	SHA256      string   `json:"sha256,omitempty"`
}

// CatalogCachePath default on-disk cache for the index.
func CatalogCachePath() string {
	if h := strings.TrimSpace(os.Getenv("GRAIN_HOME")); h != "" {
		return filepath.Join(expandHome(h), "cache", "recipes-catalog.json")
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(".grain", "cache", "recipes-catalog.json")
	}
	return filepath.Join(home, ".grain", "cache", "recipes-catalog.json")
}

// CatalogURL returns configured or default catalog index URL.
func CatalogURL() string {
	if u := strings.TrimSpace(os.Getenv("GRAIN_RECIPE_CATALOG_URL")); u != "" {
		return u
	}
	return DefaultCatalogURL
}

// HTTPDoer is injectable for tests.
type HTTPDoer interface {
	Do(req *http.Request) (*http.Response, error)
}

// FetchCatalog loads the official index. Tries network first; on failure returns
// cached index if present (and err describing network failure only if no cache).
func FetchCatalog(client HTTPDoer, catalogURL, cachePath string) (*Catalog, error) {
	if catalogURL == "" {
		catalogURL = CatalogURL()
	}
	if cachePath == "" {
		cachePath = CatalogCachePath()
	}
	if client == nil {
		client = &http.Client{Timeout: 30 * time.Second}
	}

	body, err := httpGet(client, catalogURL)
	if err != nil {
		// offline: cache
		if b, rerr := os.ReadFile(cachePath); rerr == nil {
			cat, perr := ParseCatalog(b)
			if perr == nil {
				cat.BaseURL = baseURLOf(catalogURL)
				return cat, nil
			}
		}
		return nil, fmt.Errorf("fetch catalog: %w", err)
	}
	cat, err := ParseCatalog(body)
	if err != nil {
		return nil, err
	}
	cat.BaseURL = baseURLOf(catalogURL)
	_ = os.MkdirAll(filepath.Dir(cachePath), 0o755)
	_ = os.WriteFile(cachePath, body, 0o644)
	return cat, nil
}

// ParseCatalog unmarshals index JSON.
func ParseCatalog(data []byte) (*Catalog, error) {
	var c Catalog
	if err := json.Unmarshal(data, &c); err != nil {
		return nil, fmt.Errorf("parse catalog: %w", err)
	}
	if strings.TrimSpace(c.APIVersion) != "" && c.APIVersion != CatalogAPIVersion {
		return nil, fmt.Errorf("unsupported catalog apiVersion %q (want %s)", c.APIVersion, CatalogAPIVersion)
	}
	if c.APIVersion == "" {
		c.APIVersion = CatalogAPIVersion
	}
	return &c, nil
}

// ResolveEntryURL builds the body download URL for an entry.
func (c *Catalog) ResolveEntryURL(e CatalogEntry) (string, error) {
	if u := strings.TrimSpace(e.URL); u != "" {
		return u, nil
	}
	p := strings.TrimSpace(e.Path)
	if p == "" {
		return "", fmt.Errorf("catalog entry %q has no path or url", e.ID)
	}
	if strings.HasPrefix(p, "http://") || strings.HasPrefix(p, "https://") {
		return p, nil
	}
	base := strings.TrimRight(c.BaseURL, "/")
	if base == "" {
		return "", fmt.Errorf("catalog entry %q: relative path without base URL", e.ID)
	}
	return base + "/" + strings.TrimLeft(p, "/"), nil
}

// AddFromURL downloads YAML from url, validates, optional expectedSHA256, saves to library.
// Does not create a VM.
func AddFromURL(client HTTPDoer, libDir, url, expectedSHA256 string, opts SaveOptions) (LibraryEntry, error) {
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return LibraryEntry{}, fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return LibraryEntry{}, fmt.Errorf("url must be http or https")
	}
	body, err := httpGet(client, url)
	if err != nil {
		return LibraryEntry{}, err
	}
	if len(body) > 2*1024*1024 {
		return LibraryEntry{}, fmt.Errorf("recipe too large (%d bytes)", len(body))
	}
	if exp := strings.TrimSpace(expectedSHA256); exp != "" {
		sum := sha256.Sum256(body)
		got := hex.EncodeToString(sum[:])
		if !strings.EqualFold(got, strings.TrimPrefix(exp, "sha256:")) {
			return LibraryEntry{}, fmt.Errorf("sha256 mismatch: got %s want %s", got, exp)
		}
	}
	if opts.ID == "" {
		// derive from URL path
		base := filepath.Base(strings.Split(url, "?")[0])
		opts.ID = libraryIDFromFilename(base)
	}
	return SaveLibrary(libDir, body, opts)
}

// AddFromCatalog installs one official recipe by id into the library.
func AddFromCatalog(client HTTPDoer, cat *Catalog, libDir, id string, opts SaveOptions) (LibraryEntry, error) {
	if cat == nil {
		return LibraryEntry{}, fmt.Errorf("catalog is nil")
	}
	id = strings.TrimSpace(id)
	var entry *CatalogEntry
	for i := range cat.Recipes {
		if cat.Recipes[i].ID == id {
			entry = &cat.Recipes[i]
			break
		}
	}
	if entry == nil {
		return LibraryEntry{}, fmt.Errorf("catalog has no recipe %q", id)
	}
	u, err := cat.ResolveEntryURL(*entry)
	if err != nil {
		return LibraryEntry{}, err
	}
	if opts.ID == "" {
		opts.ID = entry.ID
	}
	return AddFromURL(client, libDir, u, entry.SHA256, opts)
}

func httpGet(client HTTPDoer, url string) ([]byte, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json, text/yaml, text/plain, */*")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}
	return io.ReadAll(io.LimitReader(resp.Body, 3*1024*1024))
}

func baseURLOf(catalogURL string) string {
	// strip last path segment (catalog.json) → directory URL
	u := strings.TrimSpace(catalogURL)
	i := strings.LastIndex(u, "/")
	if i <= 0 {
		return u
	}
	return u[:i]
}
