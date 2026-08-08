package docsassert

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// Phrases that mark LLM-scaffolding tone in product docs. Matched
// case-insensitively on prose only (YAML front matter and fenced code stripped).
var llmBanlist = []string{
	"delve",
	"leverage",
	"utilize",
	"streamline",
	"empower",
	"robust",
	"seamless",
	"comprehensive",
	"cutting-edge",
	"battle-tested",
	"first-class",
	"out of the box",
	"under the hood",
	"deep dive",
	"let's dive",
	"let's explore",
	"it's worth noting",
	"important to note",
	"as mentioned",
	"in this guide",
	"in this section",
	"at a high level",
	"simply put",
	"without further ado",
	"you're all set",
	"happy coding",
	"happy hacking",
	"rest assured",
	"feel free to",
	"in order to",
	"this allows you",
	"this enables",
	"designed to",
	"makes it easy",
	"key benefits",
	"key takeaways",
	"deep contract",
	"mental model",
	"additionally,",
	"furthermore,",
	"moreover,",
	"in conclusion",
}

var (
	fenceRE       = regexp.MustCompile("(?s)```.*?```")
	frontMatterRE = regexp.MustCompile(`(?s)^---\n.*?\n---\n?`)
)

// stripDocProse removes YAML front matter and fenced code so banlist checks
// do not false-positive on identifiers inside examples.
func stripDocProse(raw string) string {
	s := frontMatterRE.ReplaceAllString(raw, "")
	s = fenceRE.ReplaceAllString(s, "")
	return s
}

// listMarkdownFiles returns .md paths under dir (recursive) using ReadDir only —
// avoids filepath.WalkDir + ReadFile (gosec G122 TOCTOU on walk callbacks).
func listMarkdownFiles(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		name := e.Name()
		path := filepath.Join(dir, name)
		if e.IsDir() {
			sub, err := listMarkdownFiles(path)
			if err != nil {
				return nil, err
			}
			out = append(out, sub...)
			continue
		}
		if strings.HasSuffix(name, ".md") {
			out = append(out, path)
		}
	}
	return out, nil
}

// TestMainDocsLLMProseBanlist walks shipped product docs and fails if banned
// LLM scaffolding phrases appear in body prose.
func TestMainDocsLLMProseBanlist(t *testing.T) {
	t.Parallel()
	modRoot := repoRoot(t)
	docsRoot := filepath.Join(modRoot, "docs", "content", "docs", "main")
	files, err := listMarkdownFiles(docsRoot)
	if err != nil {
		t.Fatal(err)
	}
	if len(files) == 0 {
		t.Fatal("no markdown under docs/content/docs/main")
	}
	var hits []string
	for _, path := range files {
		b, err := os.ReadFile(path) //nolint:gosec // G304: path is under module docs tree from listMarkdownFiles
		if err != nil {
			t.Fatal(err)
		}
		prose := strings.ToLower(stripDocProse(string(b)))
		rel, _ := filepath.Rel(modRoot, path)
		for _, phrase := range llmBanlist {
			if !strings.Contains(prose, phrase) {
				continue
			}
			for i, line := range strings.Split(prose, "\n") {
				if strings.Contains(line, phrase) {
					hits = append(hits, rel+": prose line ~"+strconv.Itoa(i+1)+" contains "+phrase)
					break
				}
			}
		}
	}
	if len(hits) > 0 {
		t.Fatalf("LLM prose banlist hits in docs/content/docs/main (strip fences/front matter):\n  %s",
			strings.Join(hits, "\n  "))
	}
}
