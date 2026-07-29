package vmsync

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	ignore "github.com/sabhiram/go-gitignore"
)

// syncIgnoreOpts configures how the ignore matcher is built.
type syncIgnoreOpts struct {
	// HostRoot is the absolute host sync root (for loading .grainignore / .gitignore).
	HostRoot string
	// NoDefaults skips built-in patterns (.git/).
	NoDefaults bool
	// NoGrainignore skips host .grainignore.
	NoGrainignore bool
	// NoGitignore skips host .gitignore (nested gitignore deferred to inventory walk).
	NoGitignore bool
	// Exclude is extra gitignore-style patterns (always exclude).
	Exclude []string
	// ExtraLines are pre-loaded pattern lines (tests / guest subtree patterns).
	ExtraLines []string
}

// syncIgnore matches slash-separated relative paths from the sync root.
type syncIgnore struct {
	// matchers applied in order; last matching rule wins when using CompileIgnoreLines
	// (go-gitignore supports negation with !).
	gi *ignore.GitIgnore
}

// defaultSyncIgnoreLines are built-in excludes (unless --no-defaults).
var defaultSyncIgnoreLines = []string{
	".git/",
	"**/.git/",
}

// buildSyncIgnore loads defaults + host ignore files + --exclude.
func buildSyncIgnore(opts syncIgnoreOpts) (*syncIgnore, error) {
	var lines []string
	if !opts.NoDefaults {
		lines = append(lines, defaultSyncIgnoreLines...)
	}
	if opts.HostRoot != "" {
		if !opts.NoGrainignore {
			p := filepath.Join(opts.HostRoot, ".grainignore")
			if more, err := readIgnoreFileLines(p); err != nil {
				return nil, err
			} else {
				lines = append(lines, more...)
			}
		}
		if !opts.NoGitignore {
			p := filepath.Join(opts.HostRoot, ".gitignore")
			if more, err := readIgnoreFileLines(p); err != nil {
				return nil, err
			} else {
				lines = append(lines, more...)
			}
		}
	}
	lines = append(lines, opts.ExtraLines...)
	for _, ex := range opts.Exclude {
		ex = strings.TrimSpace(ex)
		if ex == "" {
			continue
		}
		// Ensure exclude patterns always exclude (strip leading !).
		ex = strings.TrimPrefix(ex, "!")
		lines = append(lines, ex)
	}
	if len(lines) == 0 {
		// Empty ignorer: match nothing.
		return &syncIgnore{gi: ignore.CompileIgnoreLines()}, nil
	}
	return &syncIgnore{gi: ignore.CompileIgnoreLines(lines...)}, nil
}

// Match reports whether relPath (slash-separated) should be ignored.
func (s *syncIgnore) Match(relPath string) bool {
	if s == nil || s.gi == nil {
		return false
	}
	relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "./")
	if relPath == "" || relPath == "." {
		return false // never ignore the root itself
	}
	return s.gi.MatchesPath(relPath)
}

// MatchDir is like Match but ensures trailing-slash directory semantics for
// patterns that end with /. Callers may pass "foo" or "foo/" for directories.
func (s *syncIgnore) MatchDir(relPath string) bool {
	relPath = strings.TrimPrefix(filepath.ToSlash(relPath), "./")
	relPath = strings.TrimSuffix(relPath, "/")
	if s.Match(relPath) {
		return true
	}
	return s.Match(relPath + "/")
}

// deleteEligible reports whether a dest-only path may be deleted under --delete.
// Ignored dest paths are never deleted (rsync-like).
func (s *syncIgnore) deleteEligible(relPath string) bool {
	return !s.Match(relPath) && !s.MatchDir(relPath)
}

func readIgnoreFileLines(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read ignore %s: %w", path, err)
	}
	defer func() { _ = f.Close() }()
	var lines []string
	sc := bufio.NewScanner(f)
	// Allow long lines
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 1024*1024)
	for sc.Scan() {
		lines = append(lines, sc.Text())
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("read ignore %s: %w", path, err)
	}
	return lines, nil
}
