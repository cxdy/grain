// Package docsver filters product SVU release tags and builds docs version-switcher entries.
// Historical versions link to the git commit (GitHub tree), not checked-in content snapshots.
package docsver

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// productSVUTag matches grain product release tags only (svu-style).
// Accepts v0.7.0, 0.7.0, optional prerelease/build suffix.
// Rejects guest-agent, fc/qemu image, and SDK tags (e.g. fc-latest, sdk-python-v0.2.0).
var productSVUTag = regexp.MustCompile(`(?i)^v?([0-9]+\.[0-9]+\.[0-9]+(?:[-+][0-9A-Za-z.-]+)?)$`)

// IsProductSVUTag reports whether name is a product semver release tag (not image/SDK tags).
func IsProductSVUTag(name string) bool {
	name = strings.TrimSpace(name)
	if name == "" {
		return false
	}
	// Explicit non-product prefixes / names used in this repo.
	lower := strings.ToLower(name)
	if strings.HasPrefix(lower, "fc-") ||
		strings.HasPrefix(lower, "golden-") ||
		strings.HasPrefix(lower, "sdk-") ||
		strings.Contains(lower, "guest-agent") ||
		strings.Contains(lower, "qemu") ||
		strings.HasPrefix(lower, "agent-") {
		return false
	}
	return productSVUTag.MatchString(name)
}

// NormalizeVersion strips a leading "v" from a product tag for display (0.7.0).
func NormalizeVersion(tag string) string {
	tag = strings.TrimSpace(tag)
	if len(tag) > 0 && (tag[0] == 'v' || tag[0] == 'V') && IsProductSVUTag(tag) {
		return tag[1:]
	}
	if IsProductSVUTag(tag) {
		return tag
	}
	return strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
}

// TagRef is a git tag name plus resolved commit SHA.
type TagRef struct {
	Name   string // e.g. v0.7.0
	Commit string // full SHA
}

// VersionEntry is one docs version-switcher row (TOML/params.docsVersions shape).
type VersionEntry struct {
	Version  string `json:"version"`
	Label    string `json:"label"`
	Path     string `json:"path"`
	Commit   string `json:"commit,omitempty"`
	Live     bool   `json:"live,omitempty"`
	External bool   `json:"external,omitempty"`
}

// BuildSwitcher constructs switcher entries: one live site path + historical product tags as commit links.
// livePath is the single current content tree URL (e.g. /docs/main/).
// liveLabel is the product label for the live site (e.g. v0.7.0 (latest)).
// githubBase is https://github.com/org/repo (no trailing slash).
// tags should include product SVU tags; non-product tags are ignored.
// Newest product version first among historical entries; live is always first.
func BuildSwitcher(livePath, liveLabel, liveCommit, githubBase string, tags []TagRef) []VersionEntry {
	livePath = strings.TrimSpace(livePath)
	if livePath == "" {
		livePath = "/docs/main/"
	}
	if !strings.HasSuffix(livePath, "/") {
		livePath += "/"
	}
	githubBase = strings.TrimRight(strings.TrimSpace(githubBase), "/")
	if githubBase == "" {
		githubBase = "https://github.com/cxdy/grain"
	}
	liveLabel = strings.TrimSpace(liveLabel)
	if liveLabel == "" {
		liveLabel = "latest"
	}

	out := []VersionEntry{{
		Version: "main",
		Label:   liveLabel,
		Path:    livePath,
		Commit:  strings.TrimSpace(liveCommit),
		Live:    true,
	}}

	type verTag struct {
		ver    string
		commit string
		name   string
	}
	var hist []verTag
	seen := map[string]struct{}{}
	for _, t := range tags {
		if !IsProductSVUTag(t.Name) {
			continue
		}
		ver := NormalizeVersion(t.Name)
		if ver == "" {
			continue
		}
		if _, ok := seen[ver]; ok {
			continue
		}
		seen[ver] = struct{}{}
		hist = append(hist, verTag{ver: ver, commit: strings.TrimSpace(t.Commit), name: t.Name})
	}
	sort.Slice(hist, func(i, j int) bool {
		return semverLess(hist[j].ver, hist[i].ver) // newest first
	})

	for _, h := range hist {
		commit := h.commit
		path := githubBase + "/tree/" + commit
		if commit == "" {
			// Fall back to tag ref on GitHub when SHA unknown.
			path = githubBase + "/tree/" + h.name
		}
		out = append(out, VersionEntry{
			Version:  h.ver,
			Label:    "v" + h.ver,
			Path:     path,
			Commit:   commit,
			External: true,
		})
	}
	return out
}

// GitHubTreeURL returns a commit (or tag) tree URL for historical docs viewing.
func GitHubTreeURL(githubBase, commitOrRef string) string {
	githubBase = strings.TrimRight(strings.TrimSpace(githubBase), "/")
	commitOrRef = strings.TrimSpace(commitOrRef)
	if githubBase == "" {
		githubBase = "https://github.com/cxdy/grain"
	}
	if commitOrRef == "" {
		return githubBase
	}
	return fmt.Sprintf("%s/tree/%s", githubBase, commitOrRef)
}

// semverLess reports whether a < b for dotted numeric versions (prerelease ignored for order).
func semverLess(a, b string) bool {
	ap := verParts(a)
	bp := verParts(b)
	for i := 0; i < 3; i++ {
		if ap[i] != bp[i] {
			return ap[i] < bp[i]
		}
	}
	return a < b
}

func verParts(v string) [3]int {
	v = strings.SplitN(v, "-", 2)[0]
	v = strings.SplitN(v, "+", 2)[0]
	var p [3]int
	parts := strings.Split(v, ".")
	for i := 0; i < 3 && i < len(parts); i++ {
		n := 0
		for _, c := range parts[i] {
			if c < '0' || c > '9' {
				break
			}
			n = n*10 + int(c-'0')
		}
		p[i] = n
	}
	return p
}
