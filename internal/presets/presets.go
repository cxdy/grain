package presets

import (
	"embed"
	"fmt"
	"sort"
	"strings"
)

//go:embed *.yaml
var files embed.FS

// known order for stable List output (others sorted after).
var preferred = []string{"docker", "k3s", "act"}

// Get returns the embedded cloud-config userdata for a named preset.
func Get(name string) (string, error) {
	name = strings.TrimSpace(strings.ToLower(name))
	if name == "" {
		return "", fmt.Errorf("preset name is empty")
	}
	// reject path tricks
	if strings.ContainsAny(name, "/.\\") {
		return "", fmt.Errorf("unknown preset %q", name)
	}
	b, err := files.ReadFile(name + ".yaml")
	if err != nil {
		return "", fmt.Errorf("unknown preset %q", name)
	}
	s := strings.TrimSpace(string(b))
	if s == "" {
		return "", fmt.Errorf("preset %q is empty", name)
	}
	return s + "\n", nil
}

// List returns available preset names (docker, k3s, …).
func List() []string {
	entries, err := files.ReadDir(".")
	if err != nil {
		return nil
	}
	set := make(map[string]struct{}, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if !strings.HasSuffix(n, ".yaml") {
			continue
		}
		set[strings.TrimSuffix(n, ".yaml")] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for _, p := range preferred {
		if _, ok := set[p]; ok {
			out = append(out, p)
			delete(set, p)
		}
	}
	rest := make([]string, 0, len(set))
	for n := range set {
		rest = append(rest, n)
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// DefaultResources returns suggested CPUs and MemoryMB for a preset when the
// caller has not set resources (0 means no suggestion).
func DefaultResources(name string) (cpus, memoryMB int) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "k3s":
		return 2, 4096
	case "act":
		// Docker + act runners are memory-hungry; give a lab-sized box.
		return 2, 4096
	default:
		return 0, 0
	}
}

// NeedsAPIServerPort reports whether the preset should publish guest 6443
// when the user has not already done so (k3s API).
func NeedsAPIServerPort(name string) bool {
	return strings.ToLower(strings.TrimSpace(name)) == "k3s"
}
