package names

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

var safeName = regexp.MustCompile(`^[a-z][a-z0-9-]{0,62}$`)

// Valid reports whether name is a safe VM hostname.
func Valid(name string) bool {
	return safeName.MatchString(name)
}

// Next returns the next free name in prefix-N form (e.g. sbox-1, sbox-2).
// existing is a set of names already taken.
func Next(prefix string, existing map[string]struct{}) string {
	if prefix == "" {
		prefix = "sbox"
	}
	prefix = strings.TrimSuffix(prefix, "-")
	max := 0
	for name := range existing {
		if !strings.HasPrefix(name, prefix+"-") {
			continue
		}
		n, err := strconv.Atoi(strings.TrimPrefix(name, prefix+"-"))
		if err != nil {
			continue
		}
		if n > max {
			max = n
		}
	}
	return fmt.Sprintf("%s-%d", prefix, max+1)
}
