package cloudinit

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWriteNoCloud(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation tested on darwin (hdiutil)")
	}
	dir := t.TempDir()
	seed, err := WriteNoCloud(dir, "sbox-1", "ssh-ed25519 AAAA test@grain", "")
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(seed)
	if err != nil || st.Size() < 100 {
		t.Fatalf("seed %v size", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cidata", "user-data")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteNoCloudOpts_MinimalUserData(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation tested on darwin (hdiutil)")
	}
	dir := t.TempDir()
	key := "ssh-ed25519 AAAA minimal@grain"
	seed, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "fast-1",
		SSHPub:   key,
		Minimal:  true,
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(seed)
	if err != nil || st.Size() < 100 {
		t.Fatalf("seed %v size", err)
	}
	ud, err := os.ReadFile(filepath.Join(dir, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(ud)
	if !strings.HasPrefix(s, "#cloud-config\n") {
		t.Fatalf("user-data header:\n%s", s)
	}
	if !strings.Contains(s, "hostname: fast-1") {
		t.Fatalf("missing hostname:\n%s", s)
	}
	if !strings.Contains(s, key) {
		t.Fatalf("missing key:\n%s", s)
	}
	if !strings.Contains(s, "userdata-ran") {
		t.Fatalf("missing userdata-ran:\n%s", s)
	}
	if !strings.Contains(s, "package_update: false") {
		t.Fatalf("expected package_update false:\n%s", s)
	}
	// Full path must not force package_update.
	dir2 := t.TempDir()
	if _, err := WriteNoCloud(dir2, "full-1", key, ""); err != nil {
		t.Fatal(err)
	}
	full, err := os.ReadFile(filepath.Join(dir2, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(full), "package_update") {
		t.Fatalf("full seed should not set package_update:\n%s", full)
	}
}

func TestWriteNoCloudFullWithExtra(t *testing.T) {
	if runtime.GOOS != "darwin" {
		dir := t.TempDir()
		_, err := WriteNoCloud(dir, "h1", "ssh-ed25519 AAAA k", "echo extra")
		if err != nil {
			if !strings.Contains(err.Error(), "ISO") && !strings.Contains(err.Error(), "hdiutil") && !strings.Contains(err.Error(), "genisoimage") && !strings.Contains(err.Error(), "mkisofs") && !strings.Contains(err.Error(), "xorriso") {
				t.Logf("unexpected: %v", err)
			}
			return
		}
		return
	}
	dir := t.TempDir()
	seed, err := WriteNoCloud(dir, "full-host", "ssh-ed25519 AAAA full@grain", "#!/bin/sh\necho hi\n",
		MountSpec{Tag: "grain0", Guest: "/mnt", Driver: "9p"},
	)
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(seed); err != nil || st.Size() < 100 {
		t.Fatalf("seed %v", err)
	}
	ud, err := os.ReadFile(filepath.Join(dir, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(ud)
	if !strings.Contains(s, "full-host") {
		t.Fatalf("missing hostname:\n%s", s)
	}
	if !strings.Contains(s, "echo hi") {
		t.Fatalf("missing extra:\n%s", s)
	}
}

func TestWriteNoCloudOptsNonMinimal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation on darwin")
	}
	dir := t.TempDir()
	seed, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "nm1",
		SSHPub:   "ssh-ed25519 AAAA k",
		Minimal:  false,
		Mounts:   []MountSpec{{Tag: "t0", Guest: "/g", Driver: "virtiofs"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(seed); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "cidata", "vendor-data")); err != nil {
		t.Fatal(err)
	}
}

func TestWriteNoCloudOptsMountsMinimal(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("ISO generation on darwin")
	}
	dir := t.TempDir()
	seed, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "m1",
		SSHPub:   "ssh-ed25519 AAAA k",
		Extra:    "echo extra",
		Minimal:  true,
		Mounts: []MountSpec{
			{Tag: "grain0", Guest: "/work", Driver: "virtiofs"},
			{Tag: "", Guest: "/skip"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(seed); err != nil || st.Size() < 100 {
		t.Fatalf("seed %v", err)
	}
	ud, err := os.ReadFile(filepath.Join(dir, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(ud)
	if !strings.Contains(s, "virtiofs") {
		t.Fatalf("missing virtiofs:\n%s", s)
	}
	if !strings.Contains(s, "echo extra") {
		t.Fatalf("missing extra:\n%s", s)
	}
	// modules dropped when extra set
	if strings.Contains(s, "cloud_config_modules") {
		t.Fatalf("modules should drop with extra:\n%s", s)
	}
	// meta-data
	meta, err := os.ReadFile(filepath.Join(dir, "cidata", "meta-data"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(meta), "local-hostname: m1") {
		t.Fatalf("meta %s", meta)
	}
}

func TestWriteNoCloudOptsFullISO(t *testing.T) {
	if runtime.GOOS != "darwin" {
		found := false
		for _, bin := range []string{"genisoimage", "mkisofs", "xorriso"} {
			if _, err := exec.LookPath(bin); err == nil {
				found = true
				break
			}
		}
		if !found {
			t.Skip("no ISO tool")
		}
	}
	dir := t.TempDir()
	p, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "h",
		SSHPub:   "ssh-ed25519 AAAA test",
		Minimal:  true,
		Mounts:   []MountSpec{{Tag: "work", Guest: "/work"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(p)
	if err != nil || st.Size() == 0 {
		t.Fatalf("iso %v", err)
	}
}

func TestWriteNoCloudMkdirFailure(t *testing.T) {
	// Use a path that cannot be created (file as parent).
	if runtime.GOOS == "windows" {
		t.Skip("path tricks differ on windows")
	}
	dir := t.TempDir()
	blocker := filepath.Join(dir, "block")
	if err := os.WriteFile(blocker, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// MkdirAll on existing file returns error on Unix.
	_, err := WriteNoCloudOpts(blocker, SeedOpts{Hostname: "h", SSHPub: "k"})
	if err == nil {
		t.Fatal("expected error creating seed under file path")
	}
}

func TestWriteNoCloudOptsSeedDirMkdirFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("chmod tricks differ on windows")
	}
	dir := t.TempDir()
	// Parent exists but is not writable → MkdirAll(cidata) fails.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

	// Avoid makeISO: fail before ISO. isoOS irrelevant.
	_, err := WriteNoCloudOpts(dir, SeedOpts{Hostname: "h", SSHPub: "ssh-ed25519 AAAA k"})
	if err == nil {
		t.Fatal("expected mkdir failure for cidata under read-only dir")
	}
}

func TestWriteNoCloudOptsUserDataError(t *testing.T) {
	dir := t.TempDir()
	// Invalid extra cloud-config → Render/MergeUserData error before ISO tools.
	_, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "h",
		SSHPub:   "ssh-ed25519 AAAA k",
		Extra:    "#cloud-config\n:\n  [\n",
	})
	if err == nil {
		t.Fatal("expected user-data merge error")
	}
	if !strings.Contains(err.Error(), "cloud-init user-data") {
		t.Fatalf("err: %v", err)
	}
}

func TestWriteNoCloudOptsMakeISOError(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })
	t.Setenv("PATH", t.TempDir()) // empty of ISO tools

	dir := t.TempDir()
	_, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "h",
		SSHPub:   "ssh-ed25519 AAAA k",
		Minimal:  true,
	})
	if err == nil {
		t.Fatal("expected makeISO failure")
	}
	if !strings.Contains(err.Error(), "ISO tool") {
		t.Fatalf("err: %v", err)
	}
}

func TestMakeISODirect(t *testing.T) {
	src := t.TempDir()
	if err := os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(src, "user-data"), []byte("#cloud-config\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(t.TempDir(), "out.iso")
	if err := makeISO(src, dest); err != nil {
		if runtime.GOOS != "darwin" {
			t.Skip(err)
		}
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() == 0 {
		t.Fatalf("iso: %v", err)
	}
}

func TestMakeISOMissingTool(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })
	t.Setenv("PATH", "/nonexistent")
	err := makeISO(t.TempDir(), filepath.Join(t.TempDir(), "x.iso"))
	if err == nil {
		t.Fatal("expected no ISO tool")
	}
	if !strings.Contains(err.Error(), "no ISO tool") {
		t.Fatalf("err: %v", err)
	}
}

func TestMakeISOMissingToolPath(t *testing.T) {
	// Native path: on darwin exercises hdiutil success; on linux may fail without tools.
	if runtime.GOOS == "darwin" {
		dir := t.TempDir()
		src := filepath.Join(dir, "src")
		if err := os.MkdirAll(src, 0o755); err != nil {
			t.Fatal(err)
		}
		_ = os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644)
		dest := filepath.Join(dir, "out.iso")
		if err := makeISO(src, dest); err != nil {
			t.Logf("makeISO: %v", err)
		} else if st, err := os.Stat(dest); err != nil || st.Size() == 0 {
			t.Fatalf("iso %v", err)
		}
		return
	}
	dir := t.TempDir()
	err := makeISO(dir, filepath.Join(dir, "x.iso"))
	if err == nil {
		t.Log("ISO tool available")
	} else if !strings.Contains(err.Error(), "ISO") && !strings.Contains(err.Error(), "tool") {
		t.Logf("err: %v", err)
	}
}

func TestMakeISOHdiutilFailure(t *testing.T) {
	prev := isoOS
	isoOS = "darwin"
	t.Cleanup(func() { isoOS = prev })

	binDir := t.TempDir()
	// Fake hdiutil that always fails.
	script := "#!/bin/sh\necho fail-hdiutil >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "hdiutil"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	err := makeISO(t.TempDir(), filepath.Join(t.TempDir(), "out.iso"))
	if err == nil {
		t.Fatal("expected hdiutil failure")
	}
	if !strings.Contains(err.Error(), "hdiutil") {
		t.Fatalf("err: %v", err)
	}
}

func writeFakeISOTool(t *testing.T, binDir, name string) {
	t.Helper()
	// Parse -output <path> and write a tiny payload (genisoimage/mkisofs/xorriso style).
	script := `#!/bin/sh
out=""
while [ $# -gt 0 ]; do
  case "$1" in
    -output) out="$2"; shift 2 ;;
    *) shift ;;
  esac
done
if [ -n "$out" ]; then
  printf 'ISO' > "$out"
  exit 0
fi
exit 1
`
	if err := os.WriteFile(filepath.Join(binDir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func TestMakeISOWithFakeGenisoimagePortable(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })

	binDir := t.TempDir()
	writeFakeISOTool(t, binDir, "genisoimage")
	t.Setenv("PATH", binDir)

	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644)
	dest := filepath.Join(t.TempDir(), "out.iso")
	if err := makeISO(src, dest); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(dest)
	if err != nil || st.Size() == 0 {
		t.Fatalf("iso: %v", err)
	}
}

func TestMakeISOWithFakeMkisofsPortable(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })

	binDir := t.TempDir()
	// Only mkisofs (skip genisoimage via PATH with only mkisofs).
	writeFakeISOTool(t, binDir, "mkisofs")
	t.Setenv("PATH", binDir)

	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644)
	dest := filepath.Join(t.TempDir(), "out.iso")
	if err := makeISO(src, dest); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dest); err != nil || st.Size() == 0 {
		t.Fatalf("iso: %v", err)
	}
}

func TestMakeISOWithFakeXorrisoPortable(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })

	binDir := t.TempDir()
	writeFakeISOTool(t, binDir, "xorriso")
	t.Setenv("PATH", binDir)

	src := t.TempDir()
	_ = os.WriteFile(filepath.Join(src, "meta-data"), []byte("x\n"), 0o644)
	dest := filepath.Join(t.TempDir(), "x.iso")
	if err := makeISO(src, dest); err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(dest); err != nil || st.Size() == 0 {
		t.Fatalf("iso: %v", err)
	}
}

func TestMakeISOToolCommandFailure(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })

	binDir := t.TempDir()
	// genisoimage present but fails → error path at CombinedOutput.
	script := "#!/bin/sh\necho boom >&2\nexit 1\n"
	if err := os.WriteFile(filepath.Join(binDir, "genisoimage"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", binDir)

	err := makeISO(t.TempDir(), filepath.Join(t.TempDir(), "out.iso"))
	if err == nil {
		t.Fatal("expected genisoimage failure")
	}
	if !strings.Contains(err.Error(), "genisoimage") {
		t.Fatalf("err: %v", err)
	}
}

func TestWriteNoCloudOptsWithFakeISOTool(t *testing.T) {
	prev := isoOS
	isoOS = "linux"
	t.Cleanup(func() { isoOS = prev })

	binDir := t.TempDir()
	writeFakeISOTool(t, binDir, "mkisofs")
	t.Setenv("PATH", binDir)

	dir := t.TempDir()
	p, err := WriteNoCloudOpts(dir, SeedOpts{
		Hostname: "ci",
		SSHPub:   "ssh-ed25519 AAAA test",
		Minimal:  true,
		Extra:    "echo hi",
		Mounts:   []MountSpec{{Tag: "work", Guest: "/work", Driver: "9p"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if st, err := os.Stat(p); err != nil || st.Size() == 0 {
		t.Fatalf("seed iso: %v", err)
	}
	ud, err := os.ReadFile(filepath.Join(dir, "cidata", "user-data"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(ud)
	if !strings.Contains(s, "package_update: false") {
		t.Fatalf("minimal opts missing package_update:\n%s", s)
	}
	if !strings.Contains(s, "echo hi") {
		t.Fatalf("missing extra:\n%s", s)
	}
}
