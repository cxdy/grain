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

// RecipePreview is a validated remote/local recipe without writing the library.
// Used for Desktop/CLI trust UX: inspect then explicit Add.
type RecipePreview struct {
	URL            string   `json:"url,omitempty"`
	SuggestedID    string   `json:"suggested_id"`
	Name           string   `json:"name,omitempty"`
	Description    string   `json:"description,omitempty"`
	Image          string   `json:"image,omitempty"`
	CPUs           int      `json:"cpus,omitempty"`
	MemoryMB       int      `json:"memory_mb,omitempty"`
	DiskGB         int      `json:"disk_gb,omitempty"`
	Persistent     bool     `json:"persistent,omitempty"`
	HasBootstrap   bool     `json:"has_bootstrap,omitempty"`
	BootstrapSteps []string `json:"bootstrap_steps,omitempty"`
	Mounts         []string `json:"mounts,omitempty"`   // "host → guest"
	Forwards       []string `json:"forwards,omitempty"` // ":guest" or "host:guest"
	Warnings       []string `json:"warnings,omitempty"`
	YAML           string   `json:"yaml"` // full document for confirm-add (no re-fetch)
	SHA256         string   `json:"sha256,omitempty"`
}

// PreviewFromURL fetches and validates recipe YAML without writing the library or creating a VM.
func PreviewFromURL(client HTTPDoer, url, expectedSHA256 string) (RecipePreview, error) {
	var zero RecipePreview
	if client == nil {
		client = &http.Client{Timeout: 60 * time.Second}
	}
	url = strings.TrimSpace(url)
	if url == "" {
		return zero, fmt.Errorf("url is required")
	}
	if !strings.HasPrefix(url, "http://") && !strings.HasPrefix(url, "https://") {
		return zero, fmt.Errorf("url must be http or https")
	}
	body, err := httpGet(client, url)
	if err != nil {
		return zero, err
	}
	if len(body) > 2*1024*1024 {
		return zero, fmt.Errorf("recipe too large (%d bytes)", len(body))
	}
	sum := sha256.Sum256(body)
	got := hex.EncodeToString(sum[:])
	if exp := strings.TrimSpace(expectedSHA256); exp != "" {
		if !strings.EqualFold(got, strings.TrimPrefix(exp, "sha256:")) {
			return zero, fmt.Errorf("sha256 mismatch: got %s want %s", got, exp)
		}
	}
	prev, err := PreviewFromYAML(body)
	if err != nil {
		return zero, err
	}
	prev.URL = url
	prev.SHA256 = got
	if prev.SuggestedID == "" {
		base := filepath.Base(strings.Split(url, "?")[0])
		prev.SuggestedID = libraryIDFromFilename(base)
	}
	if strings.HasPrefix(url, "http://") {
		prev.Warnings = append(prev.Warnings, "cleartext HTTP — prefer HTTPS")
	}
	return prev, nil
}

// PreviewFromYAML validates bytes and builds a summary (no library write).
func PreviewFromYAML(body []byte) (RecipePreview, error) {
	var zero RecipePreview
	f, err := Parse(body)
	if err != nil {
		return zero, err
	}
	c, err := f.Compile()
	if err != nil {
		return zero, fmt.Errorf("compile: %w", err)
	}
	prev := RecipePreview{
		SuggestedID:  sanitizeRecipeID(f.Metadata.Name),
		Name:         f.Metadata.Name,
		Description:  f.Metadata.Description,
		Image:        c.Image,
		CPUs:         c.CPUs,
		MemoryMB:     c.MemoryMB,
		DiskGB:       c.DiskGB,
		Persistent:   c.Persistent,
		HasBootstrap: c.HasBootstrap,
		YAML:         string(body),
	}
	if prev.SuggestedID == "" {
		prev.SuggestedID = "recipe"
	}
	for _, s := range f.Spec.Bootstrap.Steps {
		prev.BootstrapSteps = append(prev.BootstrapSteps, strings.TrimSpace(s.Name))
	}
	for _, m := range c.Mounts {
		prev.Mounts = append(prev.Mounts, m.Host+" → "+m.Guest)
		if filepath.IsAbs(m.Host) {
			prev.Warnings = append(prev.Warnings, "absolute mount host path may not exist on this machine: "+m.Host)
		}
	}
	for _, fwd := range c.Forwards {
		if fwd.HostPort > 0 {
			prev.Forwards = append(prev.Forwards, fmt.Sprintf("%d:%d", fwd.HostPort, fwd.GuestPort))
		} else {
			prev.Forwards = append(prev.Forwards, fmt.Sprintf(":%d", fwd.GuestPort))
		}
	}
	if c.HasBootstrap || strings.TrimSpace(c.Userdata) != "" {
		prev.Warnings = append(prev.Warnings, "recipe includes bootstrap/userdata that runs scripts in the guest on deploy")
	}
	return prev, nil
}

// AddFromURL downloads YAML from url, validates, optional expectedSHA256, saves to library.
// Does not create a VM. Prefer PreviewFromURL then SaveLibrary for interactive UX.
func AddFromURL(client HTTPDoer, libDir, url, expectedSHA256 string, opts SaveOptions) (LibraryEntry, error) {
	prev, err := PreviewFromURL(client, url, expectedSHA256)
	if err != nil {
		return LibraryEntry{}, err
	}
	if opts.ID == "" {
		opts.ID = prev.SuggestedID
	}
	return SaveLibrary(libDir, []byte(prev.YAML), opts)
}

// LookupCatalogEntry returns the catalog row for id, or an error if missing.
func (c *Catalog) LookupCatalogEntry(id string) (CatalogEntry, error) {
	if c == nil {
		return CatalogEntry{}, fmt.Errorf("catalog is nil")
	}
	id = strings.TrimSpace(id)
	for i := range c.Recipes {
		if c.Recipes[i].ID == id {
			return c.Recipes[i], nil
		}
	}
	return CatalogEntry{}, fmt.Errorf("catalog has no recipe %q", id)
}

// PreviewFromCatalog fetches and validates one official recipe body without
// writing the library. Uses entry.SHA256 when set. Does not create a VM.
func PreviewFromCatalog(client HTTPDoer, cat *Catalog, id string) (RecipePreview, error) {
	var zero RecipePreview
	entry, err := cat.LookupCatalogEntry(id)
	if err != nil {
		return zero, err
	}
	u, err := cat.ResolveEntryURL(entry)
	if err != nil {
		return zero, err
	}
	prev, err := PreviewFromURL(client, u, entry.SHA256)
	if err != nil {
		return zero, err
	}
	// Prefer catalog id (stable library name) over YAML metadata.name.
	if entry.ID != "" {
		prev.SuggestedID = entry.ID
	}
	if strings.TrimSpace(prev.Description) == "" && strings.TrimSpace(entry.Description) != "" {
		prev.Description = entry.Description
	}
	if strings.TrimSpace(prev.Name) == "" && strings.TrimSpace(entry.Title) != "" {
		prev.Name = entry.Title
	}
	return prev, nil
}

// AddFromCatalog installs one official recipe by id into the library.
func AddFromCatalog(client HTTPDoer, cat *Catalog, libDir, id string, opts SaveOptions) (LibraryEntry, error) {
	if cat == nil {
		return LibraryEntry{}, fmt.Errorf("catalog is nil")
	}
	id = strings.TrimSpace(id)
	entry, err := cat.LookupCatalogEntry(id)
	if err != nil {
		return LibraryEntry{}, err
	}
	u, err := cat.ResolveEntryURL(entry)
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
