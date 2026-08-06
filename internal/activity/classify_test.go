package activity_test

import (
	"net/http"
	"testing"

	"github.com/cxdy/grain/internal/activity"
)

func TestClassify(t *testing.T) {
	t.Parallel()
	cases := []struct {
		m, p, act, tgt string
	}{
		{"POST", "/vms", "create", ""},
		{"DELETE", "/vms/tiny-1", "remove", "tiny-1"},
		{"POST", "/vms/lab/shutdown", "stop", "lab"},
		{"POST", "/vms/lab/start", "start", "lab"},
		{"POST", "/vms/lab/exec", "exec", "lab"},
		{"GET", "/vms/lab/shell", "shell", "lab"},
		{"POST", "/pool/claim", "pool claim", ""},
		{"POST", "/secrets", "secret set", ""},
	}
	for _, tc := range cases {
		a, tgt := activity.Classify(tc.m, tc.p)
		if a != tc.act || tgt != tc.tgt {
			t.Fatalf("%s %s: got %q %q want %q %q", tc.m, tc.p, a, tgt, tc.act, tc.tgt)
		}
	}
}

func TestSourceFromRequest(t *testing.T) {
	t.Parallel()
	r, _ := http.NewRequest(http.MethodPost, "/vms", nil)
	r.Header.Set("User-Agent", "grain-cli/dev")
	if activity.SourceFromRequest(r) != "cli" {
		t.Fatal(activity.SourceFromRequest(r))
	}
	r.Header.Set("X-Grain-Client", "desktop")
	if activity.SourceFromRequest(r) != "desktop" {
		t.Fatal(activity.SourceFromRequest(r))
	}
}

func TestShouldRecord(t *testing.T) {
	t.Parallel()
	if activity.ShouldRecord("GET", "/vms") {
		t.Fatal("list should not record")
	}
	if !activity.ShouldRecord("POST", "/vms") {
		t.Fatal("create should record")
	}
	if !activity.ShouldRecord("GET", "/vms/x/shell") {
		t.Fatal("shell should record")
	}
	if activity.ShouldRecord("GET", "/activity") {
		t.Fatal("activity poll should not record")
	}
}
