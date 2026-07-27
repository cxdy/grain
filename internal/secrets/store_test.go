package secrets

import (
	"encoding/base64"
	"encoding/json"
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

func TestPutRequiresDataAndValidB64(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(PutRequest{Name: "ok", DataBase64: ""}); err == nil {
		t.Fatal("expected data_base64 required")
	}
	if _, err := st.Put(PutRequest{Name: "ok", DataBase64: "!!!not-b64!!!"}); err == nil {
		t.Fatal("expected invalid data_base64")
	}
}

func TestDecodeB64RawStd(t *testing.T) {
	// RawStdEncoding (no padding)
	raw := base64.RawStdEncoding.EncodeToString([]byte("raw-secret"))
	got, err := decodeB64(raw)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "raw-secret" {
		t.Fatalf("got %q", got)
	}
}

func TestGetDeletePatchInvalidName(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"", "../x", "1bad", "has space"} {
		if _, err := st.Get(name, false); err == nil {
			t.Fatalf("Get(%q) should fail", name)
		}
		if err := st.Delete(name); err == nil {
			t.Fatalf("Delete(%q) should fail", name)
		}
		if _, err := st.Patch(name, PutRequest{}); err == nil {
			t.Fatalf("Patch(%q) should fail", name)
		}
		if _, err := st.ReadData(name); err == nil {
			t.Fatalf("ReadData(%q) should fail", name)
		}
	}
}

func TestDeleteNotFound(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := st.Delete("missing-secret"); err == nil {
		t.Fatal("expected not found")
	}
}

func TestPatchNotFoundAndInvalidB64(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Patch("nope", PutRequest{DataBase64: "YQ=="}); err == nil {
		t.Fatal("expected not found")
	}
	if _, err := st.Put(PutRequest{
		Name:       "p",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("v1")),
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Patch("p", PutRequest{DataBase64: "%%%"}); err == nil {
		t.Fatal("expected invalid b64 on patch")
	}
	// mode-only patch
	m, err := st.Patch("p", PutRequest{Mode: "0400"})
	if err != nil {
		t.Fatal(err)
	}
	if m.Mode != "0400" {
		t.Fatalf("mode %q", m.Mode)
	}
	// gid only
	g := uint32(42)
	m, err = st.Patch("p", PutRequest{GID: &g})
	if err != nil {
		t.Fatal(err)
	}
	if m.GID == nil || *m.GID != 42 {
		t.Fatalf("gid %+v", m.GID)
	}
}

func TestListSkipsNonDirsAndBrokenMeta(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	// loose file in secrets root
	if err := os.WriteFile(filepath.Join(st.Dir(), "not-a-secret"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	// dir without meta
	if err := os.MkdirAll(filepath.Join(st.Dir(), "empty-dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	// dir with bad meta
	bad := filepath.Join(st.Dir(), "badmeta")
	if err := os.MkdirAll(bad, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(bad, "meta.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	// valid secret
	if _, err := st.Put(PutRequest{
		Name:       "good",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("ok")),
	}); err != nil {
		t.Fatal(err)
	}
	list, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Name != "good" {
		t.Fatalf("list %+v", list)
	}
}

func TestReadMetaFillsName(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.Put(PutRequest{
		Name:       "n1",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("x")),
	}); err != nil {
		t.Fatal(err)
	}
	// rewrite meta without name field
	meta := Meta{Size: 1}
	b, _ := json.Marshal(meta)
	if err := os.WriteFile(st.metaPath("n1"), b, 0o600); err != nil {
		t.Fatal(err)
	}
	m, err := st.readMetaUnlocked("n1")
	if err != nil {
		t.Fatal(err)
	}
	if m.Name != "n1" {
		t.Fatalf("name filled: %q", m.Name)
	}
}

func TestDefaultModeOnPut(t *testing.T) {
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	m, err := st.Put(PutRequest{
		Name:       "defmode",
		DataBase64: base64.StdEncoding.EncodeToString([]byte("z")),
	})
	if err != nil {
		t.Fatal(err)
	}
	if m.Mode != "0600" {
		t.Fatalf("default mode %q", m.Mode)
	}
}

func TestListWhenDirRemoved(t *testing.T) {
	// Empty store after New — List should succeed with empty/nil
	st, err := New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	list, err := st.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("want empty list, got %+v", list)
	}
}

func TestValidNameEdges(t *testing.T) {
	t.Parallel()
	if !ValidName("a") {
		t.Fatal("single letter ok")
	}
	if ValidName("") {
		t.Fatal("empty invalid")
	}
	if ValidName("9start") {
		t.Fatal("digit start invalid")
	}
	// max 63 chars: 1 letter + up to 62 more
	b := make([]byte, 63)
	b[0] = 'a'
	for i := 1; i < 63; i++ {
		b[i] = 'b'
	}
	if !ValidName(string(b)) {
		t.Fatal("63-char name should be valid")
	}
	if ValidName(string(b) + "c") {
		t.Fatal("64-char name invalid")
	}
}
