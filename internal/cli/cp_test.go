package cli

import "testing"

func TestParseCPSpec(t *testing.T) {
	tests := []struct {
		in    string
		guest bool
		name  string
		path  string
	}{
		{"sbox-1:/tmp/x", true, "sbox-1", "/tmp/x"},
		{"vm:/home/ubuntu/file.txt", true, "vm", "/home/ubuntu/file.txt"},
		{"sbox-1:rel/path", true, "sbox-1", "rel/path"},
		{"./local", false, "", "./local"},
		{"/tmp/file", false, "", "/tmp/file"},
		{"file.txt", false, "", "file.txt"},
		// slash before colon → host path (e.g. weird names, not NAME:path)
		{"/home/a:b", false, "", "/home/a:b"},
		{"./a:b", false, "", "./a:b"},
		// empty path after colon is still guest
		{"sbox-1:", true, "sbox-1", ""},
	}
	for _, tt := range tests {
		got := parseCPSpec(tt.in)
		if got.Guest != tt.guest || got.Name != tt.name || got.Path != tt.path {
			t.Errorf("parseCPSpec(%q) = guest=%v name=%q path=%q, want guest=%v name=%q path=%q",
				tt.in, got.Guest, got.Name, got.Path, tt.guest, tt.name, tt.path)
		}
		if got.Raw != tt.in {
			t.Errorf("parseCPSpec(%q).Raw = %q", tt.in, got.Raw)
		}
	}
}

func TestSafeHostTarPath(t *testing.T) {
	dest := t.TempDir()
	ok, err := safeHostTarPath(dest, "a/b.txt")
	if err != nil {
		t.Fatal(err)
	}
	if ok == dest {
		t.Fatal("expected nested path")
	}
	if _, err := safeHostTarPath(dest, "../escape"); err == nil {
		t.Fatal("expected traversal error")
	}
	if _, err := safeHostTarPath(dest, "/abs"); err != nil {
		// absolute names are stripped of leading slash and joined under dest
		t.Fatalf("absolute name should be joined under dest, got %v", err)
	}
}
