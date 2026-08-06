package desktop

import (
	"fmt"
	"strings"

	"github.com/cxdy/grain/internal/recipe"
)

// RecipeForm is a simple create-shaped recipe builder (YAML still source of truth).
type RecipeForm struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Image       string `json:"image"`
	CPUs        int    `json:"cpus"`
	MemoryMB    int    `json:"memory_mb"`
	DiskGB      int    `json:"disk_gb"`
	Persistent  bool   `json:"persistent"`
	Preset      string `json:"preset"` // docker|k3s|act or empty
	// MountHost/MountGuest optional single share (host may be ".")
	MountHost  string `json:"mount_host"`
	MountGuest string `json:"mount_guest"`
	// GuestPort optional publish (host allocates)
	GuestPort int `json:"guest_port"`
	// Bootstrap shell (optional single step)
	BootstrapRun string `json:"bootstrap_run"`
}

// BuildRecipeYAML validates form fields and returns recipe YAML.
func BuildRecipeYAML(form RecipeForm) (string, error) {
	name := strings.TrimSpace(form.Name)
	if name == "" {
		name = strings.TrimSpace(form.ID)
	}
	if name == "" {
		return "", fmt.Errorf("name is required")
	}
	id := strings.TrimSpace(form.ID)
	if id == "" {
		id = name
	}
	img := strings.TrimSpace(form.Image)
	if img == "" {
		img = "grain-ubuntu"
	}
	f := &recipe.File{
		APIVersion: recipe.APIVersion,
		Kind:       recipe.KindSandbox,
		Metadata: recipe.Metadata{
			Name:        name,
			Description: strings.TrimSpace(form.Description),
		},
		Spec: recipe.Spec{
			Image:      img,
			CPUs:       form.CPUs,
			MemoryMB:   form.MemoryMB,
			DiskGB:     form.DiskGB,
			Persistent: form.Persistent,
			Preset:     strings.TrimSpace(form.Preset),
		},
	}
	mh := strings.TrimSpace(form.MountHost)
	mg := strings.TrimSpace(form.MountGuest)
	if mh != "" || mg != "" {
		if mh == "" || mg == "" {
			return "", fmt.Errorf("mount requires both host and guest")
		}
		f.Spec.Mounts = []recipe.Mount{{Host: mh, Guest: mg}}
	}
	if form.GuestPort > 0 {
		f.Spec.Forwards = []recipe.Forward{{GuestPort: form.GuestPort}}
	}
	if run := strings.TrimSpace(form.BootstrapRun); run != "" {
		f.Spec.Bootstrap.Steps = []recipe.Step{{
			Name:    "setup",
			Message: "bootstrap",
			Run:     run,
		}}
		f.Spec.ReadyTimeout = "15m"
	} else if f.Spec.Preset != "" {
		f.Spec.Wait = "userdata"
		f.Spec.ReadyTimeout = "15m"
	} else {
		f.Spec.Wait = "agent"
	}
	if err := f.Validate(); err != nil {
		return "", err
	}
	if _, err := f.Compile(); err != nil {
		return "", err
	}
	b, err := f.MarshalYAML()
	if err != nil {
		return "", err
	}
	_ = id
	return string(b), nil
}

// SaveRecipeForm builds YAML and installs into the library.
func (s *Service) SaveRecipeForm(form RecipeForm, overwrite bool) (RecipeInfo, error) {
	y, err := BuildRecipeYAML(form)
	if err != nil {
		return RecipeInfo{}, err
	}
	id := strings.TrimSpace(form.ID)
	if id == "" {
		id = strings.TrimSpace(form.Name)
	}
	ent, err := recipe.SaveLibrary(recipe.DefaultLibraryDir(), []byte(y), recipe.SaveOptions{
		Overwrite: overwrite,
		ID:        id,
	})
	if err != nil {
		return RecipeInfo{}, err
	}
	return RecipeInfo{
		ID: ent.ID, Path: ent.Path, Name: ent.Name, Description: ent.Description,
		Image: ent.Image, HasBootstrap: ent.HasBootstrap, InLibrary: true, Source: "library",
	}, nil
}
