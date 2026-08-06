package desktop

import (
	"context"
	"fmt"
	"sort"

	"github.com/cxdy/grain/internal/image"
)

// ImageInfo is a catalog/local image for the Desktop UI.
type ImageInfo struct {
	ID          string `json:"id"`
	Description string `json:"description"`
	Ready       bool   `json:"ready"`
	LocalOnly   bool   `json:"local_only"`
	HasAgent    bool   `json:"has_agent"`
	Pullable    bool   `json:"pullable"`
	// Version is a short installed label when ready, else empty (UI shows —).
	Version string `json:"version,omitempty"`
}

// ListImages returns catalog entries with ready status under dataDir.
func ListImages(dataDir string) []ImageInfo {
	m := image.NewManager(dataDir)
	cat := image.Catalog()
	ids := make([]string, 0, len(cat))
	for id := range cat {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ImageInfo, 0, len(ids)+4)
	seen := map[string]struct{}{}
	for _, id := range ids {
		spec := cat[id]
		ready := m.Ready(id)
		out = append(out, ImageInfo{
			ID:          id,
			Description: spec.Description,
			Ready:       ready,
			LocalOnly:   spec.LocalOnly,
			HasAgent:    m.ImageHasAgent(id),
			Pullable:    !spec.LocalOnly && spec.URL != "",
			Version:     imageVersionLabel(id, ready),
		})
		seen[id] = struct{}{}
	}
	// Local-only imports not in catalog
	local, _ := m.ListLocal()
	for _, id := range local {
		if _, ok := seen[id]; ok {
			continue
		}
		out = append(out, ImageInfo{
			ID:          id,
			Description: "local import",
			Ready:       true,
			LocalOnly:   true,
			HasAgent:    m.ImageHasAgent(id),
			Pullable:    false,
			Version:     "installed",
		})
	}
	return out
}

func imageVersionLabel(id string, ready bool) string {
	if !ready {
		return ""
	}
	// Known catalog version strings (honest labels when on disk).
	switch id {
	case "ubuntu-cloud", "grain-ubuntu":
		return "24.04"
	case "alpine-cloud":
		return "3.24"
	case "grain-ubuntu-fc", "fc-kernel":
		return "fc"
	default:
		return "installed"
	}
}

// PullImage downloads a catalog image into dataDir (local host).
func PullImage(ctx context.Context, dataDir, id string) error {
	return PullImageProgress(ctx, dataDir, id, nil)
}

// PullImageProgress downloads with optional progress callback (written, total bytes).
func PullImageProgress(ctx context.Context, dataDir, id string, progress func(written, total int64)) error {
	if id == "" {
		return fmt.Errorf("image id required")
	}
	m := image.NewManager(dataDir)
	return m.Pull(ctx, id, progress)
}

// ReadyImages returns ids that can be used for create (on disk).
func ReadyImages(dataDir string) []string {
	ready := make([]string, 0)
	for _, img := range ListImages(dataDir) {
		if img.Ready {
			ready = append(ready, img.ID)
		}
	}
	return ready
}
