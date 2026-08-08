package docsassert

import (
	"os"
	"path/filepath"
	"regexp"
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
	fenceRE      = regexp.MustCompile("(?s)```.*?```")
	frontMatterRE = regexp.MustCompile(`(?s)^---\n.*?\n---\n?`)
)

// stripDocProse removes YAML front matter and fenced code so banlist checks
// do not false-positive on identifiers inside examples.
func stripDocProse(raw string) string {
	s := frontMatterRE.ReplaceAllString(raw, "")
	s = fenceRE.ReplaceAllString(s, "")
	return s
}

// TestMainDocsLLMProseBanlist walks shipped product docs and fails if banned
// LLM scaffolding phrases appear in body prose.
func TestMainDocsLLMProseBanlist(t *testing.T) {
	t.Parallel()
	root := filepath.Join(repoRoot(t), "docs", "content", "docs", "main")
	var hits []string
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".md") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		prose := strings.ToLower(stripDocProse(string(b)))
		rel, _ := filepath.Rel(repoRoot(t), path)
		for _, phrase := range llmBanlist {
			if strings.Contains(prose, phrase) {
				// Find a line for the failure message
				for i, line := range strings.Split(prose, "\n") {
					if strings.Contains(line, phrase) {
						hits = append(hits, rel+": prose line ~"+itoa(i+1)+" contains "+phrase)
						break
					}
				}
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) > 0 {
		t.Fatalf("LLM prose banlist hits in docs/content/docs/main (strip fences/front matter):\n  %s",
			strings.Join(hits, "\n  "))
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
