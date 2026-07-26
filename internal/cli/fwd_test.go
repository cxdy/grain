package cli

import (
	"testing"
)

func TestParsePublishFlagHostGuest(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag("8080:80")
	if err != nil {
		t.Fatal(err)
	}
	if f.HostPort != 8080 || f.GuestPort != 80 {
		t.Fatalf("got %+v", f)
	}
	if f.Proto != "tcp" {
		t.Fatalf("proto %s", f.Proto)
	}
}

func TestParsePublishFlagGuestOnly(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag("80")
	if err != nil {
		t.Fatal(err)
	}
	if f.HostPort != 0 || f.GuestPort != 80 {
		t.Fatalf("got %+v", f)
	}
}

func TestParsePublishFlagColonGuestOnly(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag(":443")
	if err != nil {
		t.Fatal(err)
	}
	if f.HostPort != 0 || f.GuestPort != 443 {
		t.Fatalf("got %+v", f)
	}
}

func TestParsePublishFlagProto(t *testing.T) {
	t.Parallel()
	f, err := parsePublishFlag("udp/5353:53")
	if err != nil {
		t.Fatal(err)
	}
	if f.Proto != "udp" || f.HostPort != 5353 || f.GuestPort != 53 {
		t.Fatalf("got %+v", f)
	}
}

func TestParsePublishFlagPrivilegedHost(t *testing.T) {
	t.Parallel()
	if _, err := parsePublishFlag("80:80"); err == nil {
		t.Fatal("expected privileged host port error")
	}
	if _, err := parsePublishFlag("443:443"); err == nil {
		t.Fatal("expected privileged host port error")
	}
}

func TestParsePublishFlagInvalid(t *testing.T) {
	t.Parallel()
	for _, s := range []string{"", "abc", "1:2:3", "0:0", "-1:80"} {
		if _, err := parsePublishFlag(s); err == nil {
			t.Fatalf("expected error for %q", s)
		}
	}
}

func TestParsePublishFlagsMultiple(t *testing.T) {
	t.Parallel()
	fwds, err := parsePublishFlags([]string{"8080:80", "90"})
	if err != nil {
		t.Fatal(err)
	}
	if len(fwds) != 2 {
		t.Fatalf("len %d", len(fwds))
	}
	if fwds[0].HostPort != 8080 || fwds[0].GuestPort != 80 {
		t.Fatalf("0: %+v", fwds[0])
	}
	if fwds[1].HostPort != 0 || fwds[1].GuestPort != 90 {
		t.Fatalf("1: %+v", fwds[1])
	}
}

func TestParsePublishFlagsEmpty(t *testing.T) {
	t.Parallel()
	fwds, err := parsePublishFlags(nil)
	if err != nil || fwds != nil {
		t.Fatalf("got %v %v", fwds, err)
	}
}
