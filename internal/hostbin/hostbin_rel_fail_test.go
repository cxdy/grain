package hostbin

import (
	"path/filepath"
	"testing"
)

func TestLookPathRelativeMissing(t *testing.T) {
	t.Parallel()
	// relative with separator → exec.LookPath, missing → error (lines covering fallback path)
	if _, err := LookPath(filepath.Join("no", "such", "tool-xyz")); err == nil {
		t.Fatal("expected not found")
	}
}
