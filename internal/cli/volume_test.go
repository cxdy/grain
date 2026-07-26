package cli

import (
	"testing"
)

func TestParseVolumeFlagHostGuest(t *testing.T) {
	t.Parallel()
	m, err := parseVolumeFlag("/Users/me/src:/mnt/src")
	if err != nil {
		t.Fatal(err)
	}
	if m.Host != "/Users/me/src" || m.Guest != "/mnt/src" {
		t.Fatalf("got %+v", m)
	}
	if m.Tag != "" {
		t.Fatalf("tag should be empty until manager assigns: %q", m.Tag)
	}
}

func TestParseVolumeFlagDot(t *testing.T) {
	t.Parallel()
	m, err := parseVolumeFlag(".:/work")
	if err != nil {
		t.Fatal(err)
	}
	if m.Host != "." || m.Guest != "/work" {
		t.Fatalf("got %+v", m)
	}
}

func TestParseVolumeFlagRelative(t *testing.T) {
	t.Parallel()
	m, err := parseVolumeFlag("./src:/src")
	if err != nil {
		t.Fatal(err)
	}
	if m.Host != "./src" || m.Guest != "/src" {
		t.Fatalf("got %+v", m)
	}
}

func TestParseVolumeFlagRelativeGuestRejected(t *testing.T) {
	t.Parallel()
	if _, err := parseVolumeFlag("/host:rel"); err == nil {
		t.Fatal("expected error for non-absolute guest")
	}
}

func TestParseVolumeFlagInvalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "nocolon", ":guest", "host:", "host:guest"} {
		if _, err := parseVolumeFlag(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestParseVolumeFlagsMultiple(t *testing.T) {
	t.Parallel()
	ms, err := parseVolumeFlags([]string{".:/work", "/tmp/data:/data"})
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("len %d", len(ms))
	}
	if ms[0].Host != "." || ms[0].Guest != "/work" {
		t.Fatalf("0: %+v", ms[0])
	}
	if ms[1].Host != "/tmp/data" || ms[1].Guest != "/data" {
		t.Fatalf("1: %+v", ms[1])
	}
}

func TestParseVolumeFlagsEmpty(t *testing.T) {
	t.Parallel()
	ms, err := parseVolumeFlags(nil)
	if err != nil || ms != nil {
		t.Fatalf("got %v %v", ms, err)
	}
}
