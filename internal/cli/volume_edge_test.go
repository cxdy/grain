package cli

import "testing"

func TestParseVolumeFlagEdges(t *testing.T) {
	t.Parallel()
	// empty host/guest after split are theoretically hard; exercise absolute guest ok
	m, err := parseVolumeFlag("./data:/mnt/data")
	if err != nil || m.Host != "./data" || m.Guest != "/mnt/data" {
		t.Fatalf("%+v %v", m, err)
	}
	if _, err := parseVolumeFlag("host:relative"); err == nil {
		t.Fatal("guest must be absolute")
	}
	if _, err := parseVolumeFlag(""); err == nil {
		t.Fatal("empty")
	}
	if _, err := parseVolumeFlag("nocolon"); err == nil {
		t.Fatal("nocolon")
	}
	ms, err := parseVolumeFlags([]string{"./a:/a", "./b:/b"})
	if err != nil || len(ms) != 2 {
		t.Fatalf("%v %v", ms, err)
	}
	if _, err := parseVolumeFlags([]string{"bad"}); err == nil {
		t.Fatal("bad list")
	}
	if out, err := parseVolumeFlags(nil); err != nil || out != nil {
		t.Fatalf("%v %v", out, err)
	}
}
