package docsver

import (
	"strings"
	"testing"
)

func TestIsProductSVUTag(t *testing.T) {
	t.Parallel()
	yes := []string{"v0.7.0", "0.7.0", "v1.2.3", "v0.1.0-rc.1", "V0.2.2"}
	for _, s := range yes {
		if !IsProductSVUTag(s) {
			t.Errorf("want product: %q", s)
		}
	}
	no := []string{
		"", "main", "fc-latest", "golden-latest", "sdk-python-v0.2.0", "sdk-ts-v0.2.0",
		"guest-agent-v1", "agent-v1.0.0", "qemu-8.0", "v", "not-a-tag", "release-0.7.0",
	}
	for _, s := range no {
		if IsProductSVUTag(s) {
			t.Errorf("want non-product: %q", s)
		}
	}
}

func TestNormalizeVersion(t *testing.T) {
	t.Parallel()
	if g := NormalizeVersion("v0.7.0"); g != "0.7.0" {
		t.Fatal(g)
	}
	if g := NormalizeVersion("0.7.0"); g != "0.7.0" {
		t.Fatal(g)
	}
}

func TestBuildSwitcher(t *testing.T) {
	t.Parallel()
	tags := []TagRef{
		{Name: "v0.7.0", Commit: "aaa111"},
		{Name: "v0.6.0", Commit: "bbb222"},
		{Name: "fc-latest", Commit: "ccc"},
		{Name: "sdk-python-v0.2.0", Commit: "ddd"},
		{Name: "v0.5.0", Commit: "eee333"},
		{Name: "golden-latest", Commit: "fff"},
	}
	entries := BuildSwitcher(
		"/docs/main/",
		"v0.7.0 (latest)",
		"livecommit",
		"https://github.com/cxdy/grain",
		tags,
	)
	if len(entries) != 4 { // live + 0.7 + 0.6 + 0.5
		t.Fatalf("len=%d %+v", len(entries), entries)
	}
	if !entries[0].Live || entries[0].Path != "/docs/main/" || entries[0].Commit != "livecommit" {
		t.Fatalf("live: %+v", entries[0])
	}
	if entries[0].Label != "v0.7.0 (latest)" {
		t.Fatalf("label: %s", entries[0].Label)
	}
	// Newest historical first among externals
	if entries[1].Version != "0.7.0" || !entries[1].External {
		t.Fatalf("hist0: %+v", entries[1])
	}
	if !strings.HasPrefix(entries[1].Path, "https://github.com/cxdy/grain/tree/") {
		t.Fatalf("path: %s", entries[1].Path)
	}
	if !strings.Contains(entries[1].Path, "aaa111") {
		t.Fatalf("want commit in path: %s", entries[1].Path)
	}
	if entries[2].Version != "0.6.0" || entries[3].Version != "0.5.0" {
		t.Fatalf("order: %+v", entries)
	}
	// Non-product tags excluded
	for _, e := range entries {
		if strings.Contains(e.Version, "fc") || strings.Contains(e.Label, "sdk") {
			t.Fatalf("leaked non-product: %+v", e)
		}
	}
}

func TestGitHubTreeURL(t *testing.T) {
	t.Parallel()
	u := GitHubTreeURL("https://github.com/cxdy/grain/", "abc123")
	if u != "https://github.com/cxdy/grain/tree/abc123" {
		t.Fatal(u)
	}
}

func TestBuildSwitcherEmptyTags(t *testing.T) {
	t.Parallel()
	e := BuildSwitcher("/docs/main", "latest", "c", "", nil)
	if len(e) != 1 || !e[0].Live || e[0].Path != "/docs/main/" {
		t.Fatalf("%+v", e)
	}
}
