package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/cxdy/grain/internal/image"
)

// ImageInfo is a catalog entry with local readiness for MCP.
type ImageInfo struct {
	ID          string `json:"id"`
	Local       bool   `json:"local"`
	HasAgent    bool   `json:"has_agent"`
	Description string `json:"description,omitempty"`
	LocalOnly   bool   `json:"local_only,omitempty"`
}

// HostOps are host-local operations not available on the daemon OpenAPI.
// Tests may inject fakes; production uses LocalHost.
type HostOps interface {
	ImageList() ([]ImageInfo, error)
	ImagePull(ctx context.Context, id string) error
	// ReadVMLog returns up to maxBytes of serial (or qemu) log for name.
	ReadVMLog(name string, qemu bool, maxBytes int) (path string, content string, err error)
	DataDir() string
}

// LocalHost implements HostOps against a grain data directory.
type LocalHost struct {
	Dir string
}

// NewLocalHost uses dir (default ~/.grain when empty).
func NewLocalHost(dir string) *LocalHost {
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".grain")
	}
	return &LocalHost{Dir: dir}
}

func (h *LocalHost) DataDir() string { return h.Dir }

func (h *LocalHost) ImageList() ([]ImageInfo, error) {
	cat := image.Catalog()
	m := image.NewManager(h.Dir)
	ids := make([]string, 0, len(cat))
	for id := range cat {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]ImageInfo, 0, len(ids))
	for _, id := range ids {
		spec := cat[id]
		info := ImageInfo{
			ID:          id,
			Local:       m.Ready(id),
			HasAgent:    m.ImageHasAgent(id) || spec.HasAgent,
			Description: spec.Description,
			LocalOnly:   spec.LocalOnly,
		}
		if spec.LocalOnly {
			info.Description += " (import)"
		} else if spec.URL == "" {
			info.Description += " (unavailable on " + runtime.GOARCH + ")"
		}
		out = append(out, info)
	}
	return out, nil
}

func (h *LocalHost) ImagePull(ctx context.Context, id string) error {
	if err := os.MkdirAll(h.Dir, 0o755); err != nil {
		return err
	}
	m := image.NewManager(h.Dir)
	spec, err := image.Get(id)
	if err != nil {
		return err
	}
	if spec.LocalOnly {
		return fmt.Errorf("image %q is local-only — import with: grain image import <path> --id %s", id, id)
	}
	if spec.URL == "" {
		return fmt.Errorf("image %q cannot be pulled on %s", id, runtime.GOARCH)
	}
	return m.Pull(ctx, id, nil)
}

func (h *LocalHost) ReadVMLog(name string, qemu bool, maxBytes int) (string, string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", "", fmt.Errorf("name is required")
	}
	if maxBytes <= 0 {
		maxBytes = 256 * 1024
	}
	var path string
	if qemu {
		path = filepath.Join(h.Dir, "logs", name+".log")
	} else {
		path = filepath.Join(h.Dir, "vms", name, "serial.log")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return path, "", fmt.Errorf("no log at %s (vm not started?)", path)
		}
		return path, "", err
	}
	if len(b) > maxBytes {
		b = b[len(b)-maxBytes:]
	}
	return path, string(b), nil
}
