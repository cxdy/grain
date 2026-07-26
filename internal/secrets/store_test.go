package secrets

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

func TestPutGetListDelete(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	payload := []byte("super-secret-value")
	m, err := st.Put(PutRequest{
		Name:       "db-pass",
		DataBase64: base64.StdEncoding.EncodeToString(payload),
		Mode:       "0400",
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "db-pass" || m.Size != int64(len(payload)) || m.Mode != "0400" {
		t.Fatalf("meta %+v", m)
	}

	// Files must be 0600 / dir 0700.
	info, err := os.Stat(st.dataPath("db-pass"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("data mode %o", info.Mode().Perm())
	}

	sec, err := st.Get("db-pass", true)
	if err != nil {
		t.Fatal(err)
	}
	got, err := base64.StdEncoding.DecodeString(sec.DataBase64)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("data %q", got)
	}

	list, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "db-pass" {
		t.Fatalf("list %+v", list)
	}

	// List entries must not include data (Meta only).
	raw, err := st.Get("db-pass", false)
	if err != nil {
		t.Fatal(err)
	}
	if raw.DataBase64 != "" {
		t.Fatal("expected no data when includeData=false")
	}

	if err := st.Delete("db-pass"); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Get("db-pass", false); err == nil {
		t.Fatal("expected not found after delete")
	}
}

func TestPatch(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(PutRequest{
		Name:       "tok",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("v1")),
	}); err != nil {
		t.Fatal(err)
	}
	uid := uint32(1000)
	m, err := st.Patch("tok", PutRequest{
		DataBase64: base64.StdEncoding.EncodeToString([]byte("v2")),
		Mode:       "0640",
		UID:        &uid,
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Mode != "0640" || m.UID == nil || *m.UID != 1000 {
		t.Fatalf("meta %+v", m)
	}
	b, err := st.ReadData("tok")
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "v2" {
		t.Fatalf("data %q", b)
	}
}

func TestInvalidName(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(PutRequest{Name: "../evil", DataBase64: "YQ=="}); err == nil {
		t.Fatal("expected invalid name")
	}
	if ValidName("ok_secret.1") != true {
		t.Fatal("ok_secret.1 should be valid")
	}
}

func TestDirPermissions(t *testing.T) {
	root := t.TempDir()
	st, err := New(root)
	if err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(st.Dir())
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o700 {
		t.Fatalf("secrets dir mode %o want 0700", info.Mode().Perm())
	}
	// Ensure path is under data dir.
	if filepath.Base(st.Dir()) != "secrets" {
		t.Fatalf("dir %s", st.Dir())
	}
}
