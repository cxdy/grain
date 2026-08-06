package desktop

import (
	"context"
	"fmt"
	"strings"

	"github.com/cxdy/grain/client"
	"github.com/cxdy/grain/internal/recipe"
)

// RecipeInfo is a library or catalog row for the Desktop UI.
type RecipeInfo struct {
	ID           string   `json:"id"`
	Path         string   `json:"path,omitempty"`
	Name         string   `json:"name,omitempty"`
	Title        string   `json:"title,omitempty"`
	Description  string   `json:"description,omitempty"`
	Image        string   `json:"image,omitempty"`
	CPUs         int      `json:"cpus,omitempty"`
	MemoryMB     int      `json:"memory_mb,omitempty"`
	DiskGB       int      `json:"disk_gb,omitempty"`
	Persistent   bool     `json:"persistent,omitempty"`
	HasBootstrap bool     `json:"has_bootstrap,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	InLibrary    bool     `json:"in_library,omitempty"`
	SHA256       string   `json:"sha256,omitempty"`
	Source       string   `json:"source,omitempty"` // library|catalog
}

// DeployRecipeOpts controls create-from-recipe from Desktop.
type DeployRecipeOpts struct {
	Recipe  string `json:"recipe"`
	Name    string `json:"name,omitempty"`
	Wait    string `json:"wait,omitempty"`
	Timeout string `json:"timeout,omitempty"`
}

// ListLibraryRecipes returns ~/.grain/recipes entries.
func (s *Service) ListLibraryRecipes() ([]RecipeInfo, error) {
	list, err := recipe.ListLibrary(recipe.DefaultLibraryDir())
	if err != nil {
		return nil, err
	}
	out := make([]RecipeInfo, 0, len(list))
	for _, e := range list {
		out = append(out, RecipeInfo{
			ID: e.ID, Path: e.Path, Name: e.Name, Title: e.Name,
			Description: e.Description, Image: e.Image,
			CPUs: e.CPUs, MemoryMB: e.MemoryMB, DiskGB: e.DiskGB,
			Persistent: e.Persistent, HasBootstrap: e.HasBootstrap,
			InLibrary: true, Source: "library",
		})
	}
	return out, nil
}

// GetLibraryRecipeYAML returns raw YAML text for editing.
func (s *Service) GetLibraryRecipeYAML(id string) (string, error) {
	f, err := recipe.LoadResolved(recipe.DefaultLibraryDir(), id)
	if err != nil {
		return "", err
	}
	b, err := f.MarshalYAML()
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// SaveLibraryRecipeYAML validates and writes YAML into the library (overwrite).
func (s *Service) SaveLibraryRecipeYAML(id, yamlText string) (RecipeInfo, error) {
	ent, err := recipe.SaveLibrary(recipe.DefaultLibraryDir(), []byte(yamlText), recipe.SaveOptions{
		Overwrite: true,
		ID:        strings.TrimSpace(id),
	})
	if err != nil {
		return RecipeInfo{}, err
	}
	return RecipeInfo{
		ID: ent.ID, Path: ent.Path, Name: ent.Name, Description: ent.Description,
		Image: ent.Image, CPUs: ent.CPUs, MemoryMB: ent.MemoryMB, DiskGB: ent.DiskGB,
		Persistent: ent.Persistent, HasBootstrap: ent.HasBootstrap, InLibrary: true, Source: "library",
	}, nil
}

// DeleteLibraryRecipe removes a library file only.
func (s *Service) DeleteLibraryRecipe(id string) error {
	return recipe.DeleteLibrary(recipe.DefaultLibraryDir(), id)
}

// ImportRecipeFile copies a local path into the library.
func (s *Service) ImportRecipeFile(path string, overwrite bool) (RecipeInfo, error) {
	ent, err := recipe.AddFile(recipe.DefaultLibraryDir(), path, recipe.SaveOptions{Overwrite: overwrite})
	if err != nil {
		return RecipeInfo{}, err
	}
	return RecipeInfo{
		ID: ent.ID, Path: ent.Path, Name: ent.Name, Description: ent.Description,
		Image: ent.Image, HasBootstrap: ent.HasBootstrap, InLibrary: true, Source: "library",
	}, nil
}

// RecipeURLPreview is a validated remote recipe summary (no library write).
type RecipeURLPreview struct {
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
	Mounts         []string `json:"mounts,omitempty"`
	Forwards       []string `json:"forwards,omitempty"`
	Warnings       []string `json:"warnings,omitempty"`
	YAML           string   `json:"yaml"`
	SHA256         string   `json:"sha256,omitempty"`
}

// PreviewRecipeURL fetches and validates a recipe URL without writing the library.
func (s *Service) PreviewRecipeURL(url string) (RecipeURLPreview, error) {
	prev, err := recipe.PreviewFromURL(nil, url, "")
	if err != nil {
		return RecipeURLPreview{}, err
	}
	return RecipeURLPreview{
		URL: prev.URL, SuggestedID: prev.SuggestedID, Name: prev.Name,
		Description: prev.Description, Image: prev.Image, CPUs: prev.CPUs,
		MemoryMB: prev.MemoryMB, DiskGB: prev.DiskGB, Persistent: prev.Persistent,
		HasBootstrap: prev.HasBootstrap, BootstrapSteps: prev.BootstrapSteps,
		Mounts: prev.Mounts, Forwards: prev.Forwards, Warnings: prev.Warnings,
		YAML: prev.YAML, SHA256: prev.SHA256,
	}, nil
}

// ConfirmRecipeYAML installs a previously previewed recipe YAML into the library.
// Does not create a VM. Prefer PreviewRecipeURL then this for URL imports.
func (s *Service) ConfirmRecipeYAML(yamlText, id string, overwrite bool) (RecipeInfo, error) {
	ent, err := recipe.SaveLibrary(recipe.DefaultLibraryDir(), []byte(yamlText), recipe.SaveOptions{
		Overwrite: overwrite,
		ID:        strings.TrimSpace(id),
	})
	if err != nil {
		return RecipeInfo{}, err
	}
	return RecipeInfo{
		ID: ent.ID, Path: ent.Path, Name: ent.Name, Description: ent.Description,
		Image: ent.Image, HasBootstrap: ent.HasBootstrap, InLibrary: true, Source: "library",
	}, nil
}

// ImportRecipeURL downloads and installs a recipe (no VM create).
// Interactive Desktop should use PreviewRecipeURL then ConfirmRecipeYAML.
func (s *Service) ImportRecipeURL(url string, overwrite bool) (RecipeInfo, error) {
	ent, err := recipe.AddFromURL(nil, recipe.DefaultLibraryDir(), url, "", recipe.SaveOptions{Overwrite: overwrite})
	if err != nil {
		return RecipeInfo{}, err
	}
	return RecipeInfo{
		ID: ent.ID, Path: ent.Path, Name: ent.Name, Description: ent.Description,
		Image: ent.Image, HasBootstrap: ent.HasBootstrap, InLibrary: true, Source: "library",
	}, nil
}

// SearchOfficialRecipes returns the catalog index (cached).
func (s *Service) SearchOfficialRecipes() ([]RecipeInfo, error) {
	cat, err := recipe.FetchCatalog(nil, recipe.CatalogURL(), recipe.CatalogCachePath())
	if err != nil {
		return nil, err
	}
	lib := map[string]bool{}
	if list, err := recipe.ListLibrary(recipe.DefaultLibraryDir()); err == nil {
		for _, e := range list {
			lib[e.ID] = true
		}
	}
	out := make([]RecipeInfo, 0, len(cat.Recipes))
	for _, e := range cat.Recipes {
		out = append(out, RecipeInfo{
			ID: e.ID, Title: e.Title, Description: e.Description, Tags: e.Tags,
			SHA256: e.SHA256, InLibrary: lib[e.ID], Source: "catalog",
		})
	}
	return out, nil
}

// AddOfficialRecipe installs one catalog id into the library.
func (s *Service) AddOfficialRecipe(id string, overwrite bool) (RecipeInfo, error) {
	cat, err := recipe.FetchCatalog(nil, recipe.CatalogURL(), recipe.CatalogCachePath())
	if err != nil {
		return RecipeInfo{}, err
	}
	ent, err := recipe.AddFromCatalog(nil, cat, recipe.DefaultLibraryDir(), id, recipe.SaveOptions{Overwrite: overwrite})
	if err != nil {
		return RecipeInfo{}, err
	}
	return RecipeInfo{
		ID: ent.ID, Path: ent.Path, Name: ent.Name, Description: ent.Description,
		Image: ent.Image, HasBootstrap: ent.HasBootstrap, InLibrary: true, Source: "library",
	}, nil
}

// DeployRecipe creates a sandbox from a library recipe (name override + wait).
func (s *Service) DeployRecipe(ctx context.Context, opts DeployRecipeOpts) (*Sandbox, error) {
	rpath := strings.TrimSpace(opts.Recipe)
	if rpath == "" {
		return nil, fmt.Errorf("recipe is required")
	}
	rf, err := recipe.LoadResolved(recipe.DefaultLibraryDir(), rpath)
	if err != nil {
		return nil, err
	}
	compiled, err := rf.Compile()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(opts.Name)
	if name == "" {
		name = compiled.Name
	}
	wait := strings.TrimSpace(opts.Wait)
	if wait == "" {
		wait = compiled.Wait
	}
	if wait == "" {
		wait = client.WaitAgent
	}
	timeout := strings.TrimSpace(opts.Timeout)
	if timeout == "" {
		timeout = compiled.Timeout
	}

	co := CreateOpts{
		Name: name, Image: compiled.Image, Persistent: compiled.Persistent,
		CPUs: compiled.CPUs, MemoryMB: compiled.MemoryMB, DiskGB: compiled.DiskGB,
		Wait: wait, Timeout: timeout, Arch: compiled.Arch, GPU: compiled.GPU,
		Network: compiled.Network, Userdata: compiled.Userdata,
	}
	// mounts/forwards via Publish/Mounts string forms
	var pubs []string
	for _, f := range compiled.Forwards {
		hp := f.HostPort
		// host 0 = allocate free port at create
		pubs = append(pubs, fmt.Sprintf("%d:%d", hp, f.GuestPort))
	}
	co.Publish = strings.Join(pubs, ",")
	var mts []string
	for _, m := range compiled.Mounts {
		mts = append(mts, m.Host+":"+m.Guest)
	}
	co.Mounts = strings.Join(mts, "\n")
	return s.CreateSandbox(ctx, co)
}
