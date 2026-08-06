package recipe

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// Snapshot is create-shaped sandbox state suitable for recipe export.
// Runtime fields (SSH port, PID, status) are intentionally omitted.
type Snapshot struct {
	Name        string
	Description string
	Image       string
	CPUs        int
	MemoryMB    int
	DiskGB      int
	Persistent  bool
	Arch        string
	GPU         string
	Network     string
	Mounts      []Mount
	Forwards    []Forward
	// SocketForwards maps host unix sockets into the guest.
	SocketForwards []SocketForward
}

// FromSnapshot builds a validated recipe document from create-shaped sandbox state.
// Bootstrap steps and userdata cannot be recovered from a running VM and are left empty.
func FromSnapshot(s Snapshot) (*File, error) {
	name := strings.TrimSpace(s.Name)
	if name == "" {
		return nil, fmt.Errorf("name is required")
	}
	desc := strings.TrimSpace(s.Description)
	if desc == "" {
		desc = fmt.Sprintf("Exported from sandbox %q", name)
	}

	net := strings.TrimSpace(s.Network)
	// slirp is the default isolation mode; omit for portable recipes.
	if strings.EqualFold(net, "slirp") {
		net = ""
	}

	f := &File{
		APIVersion: APIVersion,
		Kind:       KindSandbox,
		Metadata: Metadata{
			Name:        name,
			Description: desc,
		},
		Spec: Spec{
			Image:      strings.TrimSpace(s.Image),
			CPUs:       s.CPUs,
			MemoryMB:   s.MemoryMB,
			DiskGB:     s.DiskGB,
			Persistent: s.Persistent,
			Arch:       strings.TrimSpace(s.Arch),
			GPU:        strings.TrimSpace(s.GPU),
			Network:    net,
		},
	}

	for _, m := range s.Mounts {
		host := strings.TrimSpace(m.Host)
		guest := strings.TrimSpace(m.Guest)
		if host == "" || guest == "" {
			continue
		}
		f.Spec.Mounts = append(f.Spec.Mounts, Mount{
			Host:  host,
			Guest: guest,
			Tag:   strings.TrimSpace(m.Tag),
		})
	}
	for _, fwd := range s.Forwards {
		if fwd.GuestPort <= 0 {
			continue
		}
		proto := strings.TrimSpace(fwd.Proto)
		if proto == "" || strings.EqualFold(proto, "tcp") {
			proto = ""
		}
		// HostPort 0 means "allocate" at create; omit for portability when zero.
		out := Forward{
			GuestPort: fwd.GuestPort,
			Proto:     proto,
		}
		if fwd.HostPort > 0 {
			out.HostPort = fwd.HostPort
		}
		f.Spec.Forwards = append(f.Spec.Forwards, out)
	}
	for _, sf := range s.SocketForwards {
		hp := strings.TrimSpace(sf.HostPath)
		gp := strings.TrimSpace(sf.GuestPath)
		if hp == "" || gp == "" {
			continue
		}
		f.Spec.SocketForwards = append(f.Spec.SocketForwards, SocketForward{
			HostPath:  hp,
			GuestPath: gp,
		})
	}

	if err := f.Validate(); err != nil {
		return nil, err
	}
	return f, nil
}

// MarshalYAML encodes the recipe document as YAML bytes (with trailing newline).
func (f *File) MarshalYAML() ([]byte, error) {
	if f == nil {
		return nil, fmt.Errorf("recipe is nil")
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	// Encode only the on-disk fields (SourcePath/BaseDir have yaml:"-").
	b, err := yaml.Marshal(f)
	if err != nil {
		return nil, fmt.Errorf("marshal recipe: %w", err)
	}
	// Header helps operators find docs and recreate with the CLI.
	header := "# Grain sandbox recipe (apiVersion: grain/v1, kind: Sandbox)\n" +
		"# Create: grain new --recipe <this-file>\n" +
		"# Docs:   https://grainvm.com/docs/main/get-started/recipe/\n"
	out := append([]byte(header), b...)
	if len(out) == 0 || out[len(out)-1] != '\n' {
		out = append(out, '\n')
	}
	return out, nil
}

// FormatSnapshot is a convenience: Snapshot → validated YAML.
func FormatSnapshot(s Snapshot) (string, error) {
	f, err := FromSnapshot(s)
	if err != nil {
		return "", err
	}
	b, err := f.MarshalYAML()
	if err != nil {
		return "", err
	}
	return string(b), nil
}
