package agent

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteClipboardHelpers(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := writeClipboardHelpers(dir); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"pbcopy", "pbpaste", "xclip", "xsel", "wl-copy", "wl-paste"} {
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if st.Mode()&0o111 == 0 {
			t.Fatalf("%s not executable: %v", name, st.Mode())
		}
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.HasPrefix(string(b), "#!/bin/sh") {
			t.Fatalf("%s missing shebang", name)
		}
	}
	// pbcopy must emit OSC 52
	pb, _ := os.ReadFile(filepath.Join(dir, "pbcopy"))
	if !strings.Contains(string(pb), "]52;c;") {
		t.Fatal("pbcopy missing OSC 52 sequence")
	}
}

func TestPathWithClipboardBin(t *testing.T) {
	t.Parallel()
	got := pathWithClipboardBin("PATH=/usr/bin:/bin")
	if got != "PATH="+clipboardBinDir+":/usr/bin:/bin" {
		t.Fatalf("%q", got)
	}
	// idempotent prefix
	got2 := pathWithClipboardBin(got)
	if got2 != got {
		t.Fatalf("dup path: %q", got2)
	}
	if pathWithClipboardBin("PATH=") != "PATH="+clipboardBinDir {
		t.Fatal("empty path")
	}
}

func TestEnsureClipboardHelpersIdempotent(t *testing.T) {
	// Uses real /var/lib/grain/bin when writable; skip if not.
	if err := os.MkdirAll("/var/lib/grain", 0o755); err != nil {
		t.Skip("cannot mkdir /var/lib/grain:", err)
	}
	// Reset once for test isolation — re-call write directly instead.
	dir := t.TempDir()
	if err := writeClipboardHelpers(dir); err != nil {
		t.Fatal(err)
	}
	if err := writeClipboardHelpers(dir); err != nil {
		t.Fatal(err)
	}
}
