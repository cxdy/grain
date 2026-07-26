package cli

import (
	"fmt"
	"strings"

	"github.com/cxdy/grain/internal/vm"
)

// parseVolumeFlag parses a single -v / --volume value: HOST:GUEST.
// HOST may be "." or relative; GUEST must be an absolute guest path (start with /).
func parseVolumeFlag(s string) (vm.Mount, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return vm.Mount{}, fmt.Errorf("empty --volume value")
	}
	// Split on first ':' — host paths on Unix don't contain colons.
	i := strings.Index(s, ":")
	if i <= 0 || i == len(s)-1 {
		return vm.Mount{}, fmt.Errorf("invalid volume %q (want HOST:GUEST)", s)
	}
	host := s[:i]
	guest := s[i+1:]
	if host == "" {
		return vm.Mount{}, fmt.Errorf("invalid volume %q: empty host path", s)
	}
	if guest == "" {
		return vm.Mount{}, fmt.Errorf("invalid volume %q: empty guest path", s)
	}
	if !strings.HasPrefix(guest, "/") {
		return vm.Mount{}, fmt.Errorf("invalid volume %q: guest path must be absolute (start with /)", s)
	}
	return vm.Mount{Host: host, Guest: guest}, nil
}

// parseVolumeFlags parses all -v / --volume values.
func parseVolumeFlags(vals []string) ([]vm.Mount, error) {
	if len(vals) == 0 {
		return nil, nil
	}
	out := make([]vm.Mount, 0, len(vals))
	for _, v := range vals {
		m, err := parseVolumeFlag(v)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, nil
}
