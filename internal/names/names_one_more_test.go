package names

import "testing"

func TestValidRejectsUppercaseAndLeadingDigit(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"Bad", "1abc", "a_b", "", "A"} {
		if Valid(s) {
			t.Fatalf("Valid(%q) true", s)
		}
	}
	if !Valid("ok-name-1") {
		t.Fatal()
	}
}
