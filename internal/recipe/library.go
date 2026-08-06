package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// DefaultLibraryDir returns ~/.grain/recipes (or $GRAIN_HOME/recipes).
func DefaultLibraryDir() string {
	if h := strings.TrimSpace(os.Getenv("GRAIN_HOME")); h != "" {
		return filepath.Join(expandHome(h), "recipes")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".grain", "recipes")
	}
	return filepath.Join(home, ".grain", "recipes")
}

func expandHome(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return p
	}
	if p == "~" {
		if h, err := os.UserHomeDir(); err == nil {
			return h
		}
		return p
	}
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// LibraryEntry is a recipe installed in the local library.
type LibraryEntry struct {
	ID           string // filename stem
	Path         string
	Name         string // metadata.name
	Description  string
	Image        string
	CPUs         int
	MemoryMB     int
	DiskGB       int
	Persistent   bool
	HasBootstrap bool
}

// ListLibrary scans dir for *.yaml / *.yml recipes. Invalid files are skipped
// (listed with empty Name when unreadable — only valid recipes returned).
func ListLibrary(dir string) ([]LibraryEntry, error) {
	dir = expandHome(strings.TrimSpace(dir))
	if dir == "" {
		dir = DefaultLibraryDir()
	}
	ents, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []LibraryEntry
	for _, e := range ents {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		ext := strings.ToLower(filepath.Ext(name))
		if ext != ".yaml" && ext != ".yml" {
			continue
		}
		path := filepath.Join(dir, name)
		f, err := Load(path)
		if err != nil {
			continue // skip invalid
		}
		id := libraryIDFromFilename(name)
		c, _ := f.Compile()
		entry := LibraryEntry{
			ID:          id,
			Path:        path,
			Name:        f.Metadata.Name,
			Description: f.Metadata.Description,
			Image:       f.Spec.Image,
			CPUs:        f.Spec.CPUs,
			MemoryMB:    f.Spec.MemoryMB,
			DiskGB:      f.Spec.DiskGB,
			Persistent:  f.Spec.Persistent,
		}
		if c != nil {
			entry.HasBootstrap = c.HasBootstrap
			if entry.Image == "" {
				entry.Image = c.Image
			}
		}
		out = append(out, entry)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func libraryIDFromFilename(name string) string {
	base := filepath.Base(name)
	// strip .yaml / .yml
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	// legacy: foo.recipe.yaml → id foo
	stem = strings.TrimSuffix(stem, ".recipe")
	return stem
}

// ResolvePath resolves a library name or filesystem path to an absolute recipe path.
// Order for bare names: <dir>/<name>.yaml, <dir>/<name>.yml, <dir>/<name>.recipe.yaml,
// then if path exists on disk as given.
func ResolvePath(dir, nameOrPath string) (string, error) {
	nameOrPath = strings.TrimSpace(nameOrPath)
	if nameOrPath == "" {
		return "", fmt.Errorf("recipe name or path is required")
	}
	dir = expandHome(strings.TrimSpace(dir))
	if dir == "" {
		dir = DefaultLibraryDir()
	}

	// Absolute or relative path that exists wins.
	if filepath.IsAbs(nameOrPath) || strings.Contains(nameOrPath, string(filepath.Separator)) || strings.HasPrefix(nameOrPath, "./") || strings.HasPrefix(nameOrPath, "../") {
		abs, err := filepath.Abs(nameOrPath)
		if err != nil {
			return "", err
		}
		if _, err := os.Stat(abs); err != nil {
			return "", fmt.Errorf("recipe path %s: %w", abs, err)
		}
		return abs, nil
	}
	// Bare name: also try as relative path if it exists with extension
	if st, err := os.Stat(nameOrPath); err == nil && !st.IsDir() {
		return filepath.Abs(nameOrPath)
	}

	id := libraryIDFromFilename(nameOrPath)
	candidates := []string{
		filepath.Join(dir, id+".yaml"),
		filepath.Join(dir, id+".yml"),
		filepath.Join(dir, id+".recipe.yaml"),
		filepath.Join(dir, nameOrPath),
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return filepath.Abs(c)
		}
	}
	return "", fmt.Errorf("recipe %q not found in library %s (tried %s.yaml)", id, dir, id)
}

// LoadResolved loads a recipe by library name or path.
func LoadResolved(dir, nameOrPath string) (*File, error) {
	path, err := ResolvePath(dir, nameOrPath)
	if err != nil {
		return nil, err
	}
	return Load(path)
}

// SaveOptions controls library writes.
type SaveOptions struct {
	// Overwrite allows replacing an existing library file. Default false.
	Overwrite bool
	// ID overrides the library id (filename stem). Empty → metadata.name or source stem.
	ID string
}

// SaveLibrary writes validated recipe YAML into the library. Refuses invalid recipes
// and refuses overwrite unless opts.Overwrite.
func SaveLibrary(dir string, data []byte, opts SaveOptions) (LibraryEntry, error) {
	var zero LibraryEntry
	dir = expandHome(strings.TrimSpace(dir))
	if dir == "" {
		dir = DefaultLibraryDir()
	}
	f, err := Parse(data)
	if err != nil {
		return zero, err
	}
	if _, err := f.Compile(); err != nil {
		return zero, fmt.Errorf("compile: %w", err)
	}
	id := strings.TrimSpace(opts.ID)
	if id == "" {
		id = strings.TrimSpace(f.Metadata.Name)
	}
	if id == "" {
		return zero, fmt.Errorf("recipe id required (metadata.name empty and no --id)")
	}
	// sanitize id to filename-safe token
	id = sanitizeRecipeID(id)
	if id == "" {
		return zero, fmt.Errorf("invalid recipe id")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return zero, err
	}
	path := filepath.Join(dir, id+".yaml")
	if _, err := os.Stat(path); err == nil && !opts.Overwrite {
		return zero, fmt.Errorf("recipe %q already exists at %s (use overwrite)", id, path)
	}
	// Re-marshal from File for clean output (or keep original if preferred)
	body, err := yamlBody(f)
	if err != nil {
		return zero, err
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return zero, err
	}
	return LibraryEntry{
		ID:           id,
		Path:         path,
		Name:         f.Metadata.Name,
		Description:  f.Metadata.Description,
		Image:        f.Spec.Image,
		CPUs:         f.Spec.CPUs,
		MemoryMB:     f.Spec.MemoryMB,
		DiskGB:       f.Spec.DiskGB,
		Persistent:   f.Spec.Persistent,
		HasBootstrap: len(f.Spec.Bootstrap.Steps) > 0,
	}, nil
}

// AddFile copies a local recipe file into the library after validation.
func AddFile(dir, srcPath string, opts SaveOptions) (LibraryEntry, error) {
	srcPath = expandHome(srcPath)
	b, err := os.ReadFile(srcPath)
	if err != nil {
		return LibraryEntry{}, fmt.Errorf("read %s: %w", srcPath, err)
	}
	if opts.ID == "" {
		opts.ID = libraryIDFromFilename(srcPath)
	}
	return SaveLibrary(dir, b, opts)
}

// DeleteLibrary removes a library recipe by id. Does not touch VMs.
func DeleteLibrary(dir, id string) error {
	dir = expandHome(strings.TrimSpace(dir))
	if dir == "" {
		dir = DefaultLibraryDir()
	}
	id = sanitizeRecipeID(strings.TrimSpace(id))
	if id == "" {
		return fmt.Errorf("recipe id required")
	}
	path, err := ResolvePath(dir, id)
	if err != nil {
		return err
	}
	// Only delete files under the library dir
	absDir, _ := filepath.Abs(dir)
	absPath, _ := filepath.Abs(path)
	if absDir != "" && !strings.HasPrefix(absPath, absDir+string(filepath.Separator)) && absPath != absDir {
		return fmt.Errorf("refusing to delete path outside library: %s", path)
	}
	return os.Remove(path)
}

func sanitizeRecipeID(id string) string {
	id = strings.TrimSpace(id)
	id = filepath.Base(id)
	id = strings.TrimSuffix(id, filepath.Ext(id))
	id = strings.TrimSuffix(id, ".recipe")
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		case r == ' ':
			b.WriteByte('-')
		}
	}
	return strings.Trim(b.String(), ".-_")
}

func yamlBody(f *File) ([]byte, error) {
	return f.MarshalYAML()
}
