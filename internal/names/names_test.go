package names_test

import (
	"testing"

	"github.com/cxdy/grain/internal/names"
)

func TestValid(t *testing.T) {
	t.Parallel()
	cases := []struct {
		in   string
		want bool
	}{
		{"sbox-1", true},
		{"a", true},
		{"grain-lab", true},
		{"", false},
		{"1bad", false},
		{"Bad", false},
		{"has_under", false},
		{"has space", false},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			if got := names.Valid(tc.in); got != tc.want {
				t.Fatalf("Valid(%q)=%v want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestNext(t *testing.T) {
	t.Parallel()
	ex := map[string]struct{}{}
	if got := names.Next("sbox", ex); got != "sbox-1" {
		t.Fatalf("empty: got %s", got)
	}
	ex["sbox-1"] = struct{}{}
	ex["sbox-3"] = struct{}{}
	if got := names.Next("sbox", ex); got != "sbox-4" {
		t.Fatalf("gap: got %s want sbox-4", got)
	}
	if got := names.Next("", map[string]struct{}{}); got != "sbox-1" {
		t.Fatalf("default prefix: %s", got)
	}
}
