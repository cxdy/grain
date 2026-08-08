//go:build linux

package osc52

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadClipboardLinuxWithFakeHelpers(t *testing.T) {
	dir := t.TempDir()
	// Prefer image/png path first — emit PNG magic via printf.
	wlPaste := filepath.Join(dir, "wl-paste")
	// Use octal escapes — portable /bin/sh does not honor \xHH.
	if err := os.WriteFile(wlPaste, []byte("#!/bin/sh\n"+
		"if [ \"$1\" = \"-t\" ] && [ \"$2\" = \"image/png\" ]; then\n"+
		"  printf '\\211PNG\\r\\n\\032\\n'\n"+
		"  exit 0\n"+
		"fi\n"+
		"if [ \"$1\" = \"-t\" ]; then exit 1; fi\n"+
		"echo hello-text\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
	got, err := readClipboardLinux()
	if err != nil {
		t.Fatal(err)
	}
	if !looksLikeImage(got) {
		t.Fatalf("want png got %q", got)
	}

	// No image: only text via xclip
	dir2 := t.TempDir()
	xclip := filepath.Join(dir2, "xclip")
	if err := os.WriteFile(xclip, []byte("#!/bin/sh\n"+
		"if echo \"$*\" | grep -q image; then exit 1; fi\n"+
		"echo from-xclip\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir2)
	got, err = readClipboardLinux()
	if err != nil || strings.TrimSpace(string(got)) != "from-xclip" {
		t.Fatalf("%q %v", got, err)
	}

	// jpeg branch via wl-paste (octal escapes for portable /bin/sh)
	dirJPEG := t.TempDir()
	wl2 := filepath.Join(dirJPEG, "wl-paste")
	if err := os.WriteFile(wl2, []byte("#!/bin/sh\n"+
		"if [ \"$2\" = \"image/png\" ]; then exit 1; fi\n"+
		"if [ \"$2\" = \"image/jpeg\" ]; then printf '\\377\\330\\377\\340'; exit 0; fi\n"+
		"echo text\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dirJPEG)
	got, err = readClipboardLinux()
	if err != nil || !looksLikeImage(got) {
		t.Fatalf("jpeg path %q %v", got, err)
	}

	// xsel text only
	dir3 := t.TempDir()
	xsel := filepath.Join(dir3, "xsel")
	if err := os.WriteFile(xsel, []byte("#!/bin/sh\necho from-xsel\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir3)
	got, err = readClipboardLinux()
	if err != nil || strings.TrimSpace(string(got)) != "from-xsel" {
		t.Fatalf("%q %v", got, err)
	}

	// no helpers
	t.Setenv("PATH", t.TempDir())
	if _, err := readClipboardLinux(); err == nil {
		t.Fatal("want errNoClipboard")
	}

}
