package names

import "testing"

func TestNextSkipsNonMatchingAndBadSuffix(t *testing.T) {
	t.Parallel()
	existing := map[string]struct{}{
		"other-9":      {},
		"sbox-abc":     {}, // non-numeric
		"sbox-":        {},
		"sbox-2-extra": {},
		"sbox-3":       {},
	}
	got := Next("sbox", existing)
	if got != "sbox-4" {
		t.Fatalf("got %s", got)
	}
	// empty prefix default
	if Next("", map[string]struct{}{"sbox-1": {}}) != "sbox-2" {
		t.Fatal(Next("", map[string]struct{}{"sbox-1": {}}))
	}
}
