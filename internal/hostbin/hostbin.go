// Package hostbin resolves host helper binaries with a few well-known
// install locations (Homebrew) when they are not on PATH.
package hostbin

import (
	"os"
	"os/exec"
	"path/filepath"
)

// commonDirs are checked when exec.LookPath fails. Homebrew on Apple Silicon
// uses /opt/homebrew/bin; Intel Homebrew and some Linux prefixes use /usr/local/bin.
var commonDirs = []string{
	"/opt/homebrew/bin",
	"/usr/local/bin",
}

// LookPath is like exec.LookPath, but also probes commonDirs for name when
// PATH does not include them (common with minimal shells / GUI-launched tools).
func LookPath(name string) (string, error) {
	if name == "" {
		return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
	}
	// Absolute or relative path with separator: defer to LookPath semantics.
	if filepath.Base(name) != name {
		return exec.LookPath(name)
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, dir := range commonDirs {
		cand := filepath.Join(dir, name)
		if isExecutable(cand) {
			return cand, nil
		}
	}
	return "", &exec.Error{Name: name, Err: exec.ErrNotFound}
}

// FoundOffPATH reports whether name exists under commonDirs but not on PATH.
// Used by doctor to print a helpful PATH hint.
func FoundOffPATH(name string) (path string, ok bool) {
	if name == "" || filepath.Base(name) != name {
		return "", false
	}
	if _, err := exec.LookPath(name); err == nil {
		return "", false
	}
	for _, dir := range commonDirs {
		cand := filepath.Join(dir, name)
		if isExecutable(cand) {
			return cand, true
		}
	}
	return "", false
}

func isExecutable(path string) bool {
	st, err := os.Stat(path)
	if err != nil || st.IsDir() {
		return false
	}
	// Unix: any execute bit for user/group/other is enough to try.
	return st.Mode()&0o111 != 0
}
