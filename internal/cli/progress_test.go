package cli

import (
	"testing"
	"time"
)

func TestCreateStage(t *testing.T) {
	cases := []struct {
		d    time.Duration
		want string
	}{
		{0, "disk"},
		{time.Second, "disk"},
		{3 * time.Second, "boot"},
		{10 * time.Second, "waiting ssh"},
		{2 * time.Minute, "waiting ssh"},
	}
	for _, tc := range cases {
		if got := createStage(tc.d); got != tc.want {
			t.Errorf("createStage(%v) = %q, want %q", tc.d, got, tc.want)
		}
	}
}
