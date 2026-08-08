//go:build darwin

package osc52

import (
	"os/exec"
	"testing"
)

func TestDarwinClipboardHelperIdempotent(t *testing.T) {
	p1, err1 := darwinClipboardHelper()
	p2, err2 := darwinClipboardHelper()
	if err1 != nil && err2 != nil {
		t.Logf("helper unavailable: %v", err1)
		_, _ = readDarwinClipboardImage()
		return
	}
	if p1 != p2 {
		t.Fatalf("%q vs %q", p1, p2)
	}
	_, _ = readDarwinClipboardImage()
}

func TestReadDarwinClipboardImageLive(t *testing.T) {
	if _, err := exec.LookPath("swift"); err != nil {
		t.Skip("swift required for image paste")
	}
	_, _ = readDarwinClipboardImage()
}
