package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/hostbin"
)

// Manager handles base images under dataDir/images/<id>/.
type Manager struct {
	DataDir string
	Client  *http.Client
}

func NewManager(dataDir string) *Manager {
	return &Manager{
		DataDir: dataDir,
		Client:  &http.Client{Timeout: 30 * time.Minute},
	}
}

func (m *Manager) Dir(id string) string {
	return filepath.Join(m.DataDir, "images", id)
}

// DiskPath returns the path to the base disk if present.
func (m *Manager) DiskPath(id string) (string, error) {
	dir := m.Dir(id)
	for _, name := range []string{"disk.qcow2", "disk.img", "disk.raw"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.Size() > 1024*1024 {
			return p, nil
		}
	}
	return "", fmt.Errorf("image %q not pulled (run: grain image pull %s)", id, id)
}

// Ready reports whether a usable base disk exists.
func (m *Manager) Ready(id string) bool {
	_, err := m.DiskPath(id)
	return err == nil
}

// ImageHasAgent reports whether the base image is expected to ship grain-agent.
// Order: local metadata has_agent, then catalog Spec.HasAgent.
func (m *Manager) ImageHasAgent(id string) bool {
	if b, ok := m.readHasAgentMeta(id); ok {
		return b
	}
	if spec, err := Get(id); err == nil {
		return spec.HasAgent
	}
	return false
}

func (m *Manager) readHasAgentMeta(id string) (bool, bool) {
	p := filepath.Join(m.Dir(id), "has_agent")
	b, err := os.ReadFile(p)
	if err != nil {
		return false, false
	}
	s := strings.TrimSpace(string(b))
	switch s {
	case "1", "true", "yes":
		return true, true
	case "0", "false", "no":
		return false, true
	default:
		return false, false
	}
}

func (m *Manager) writeMeta(id string, spec Spec, hasAgent bool) {
	dir := m.Dir(id)
	if spec.URL != "" {
		_ = os.WriteFile(filepath.Join(dir, "source.url"), []byte(spec.URL+"\n"), 0o644)
	}
	if spec.SSHUser != "" {
		_ = os.WriteFile(filepath.Join(dir, "ssh_user"), []byte(spec.SSHUser+"\n"), 0o644)
	}
	val := "false"
	if hasAgent {
		val = "true"
	}
	_ = os.WriteFile(filepath.Join(dir, "has_agent"), []byte(val+"\n"), 0o644)
}

// fileSHA256 returns the hex-encoded SHA256 of the file at path.
func fileSHA256(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() { _ = f.Close() }()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// VerifySHA256 hashes path and compares to want.
// If want is empty, verification is skipped (dev/AllowUnverified only —
// production pulls must not reach here with an empty want; see pullSpec).
// On mismatch the file at path is removed.
func VerifySHA256(path, want string) error {
	if want == "" {
		return nil
	}
	got, err := fileSHA256(path)
	if err != nil {
		return fmt.Errorf("sha256: %w", err)
	}
	if got != want {
		_ = os.Remove(path)
		return fmt.Errorf("sha256 mismatch: got %s want %s", got, want)
	}
	return nil
}

// Pull downloads the image if missing. progress is optional (bytes written, total hint).
func (m *Manager) Pull(ctx context.Context, id string, progress func(written, total int64)) error {
	spec, err := Get(id)
	if err != nil {
		return err
	}
	return m.pullSpec(ctx, spec, progress)
}

// pullSpec downloads and installs spec under images/<spec.ID>/.
// Used by Pull and by tests with httptest-backed Specs.
func (m *Manager) pullSpec(ctx context.Context, spec Spec, progress func(written, total int64)) error {
	id := spec.ID
	if spec.LocalOnly || spec.URL == "" {
		if m.Ready(id) {
			return nil
		}
		if spec.LocalOnly {
			return fmt.Errorf("image %q is local-only (run: grain image import <path> --id %s)", id, id)
		}
		return fmt.Errorf("image %q has no download URL for this architecture", id)
	}
	if m.Ready(id) {
		return nil
	}

	dir := m.Dir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	ext := ".img"
	if spec.Format == "qcow2" {
		ext = ".qcow2"
	}
	dest := filepath.Join(dir, "disk"+ext)
	partial := dest + ".partial"

	// Resolve expected digest: pinned Spec.SHA256, else companion URL.sha256 sidecar.
	wantSHA, err := resolveWantSHA256(ctx, m.client(), spec.URL, spec.SHA256)
	if err != nil {
		return err
	}
	// Fail closed: production catalog IDs must not install without a digest.
	// Empty pin + missing sidecar was previously a silent skip (supply-chain risk).
	if wantSHA == "" && !spec.AllowUnverified {
		return fmt.Errorf("image %q: no SHA256 pin and no companion .sha256 sidecar (refusing unverified pull; import a local disk or fix the release sidecar)", id)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	res, err := m.client().Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %s", res.Status)
	}

	f, err := os.Create(partial)
	if err != nil {
		return err
	}

	total := res.ContentLength
	if total <= 0 {
		total = spec.SizeHint
	}
	var written int64
	buf := make([]byte, 256*1024)
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				_ = f.Close()
				_ = os.Remove(partial)
				return err
			}
			written += int64(n)
			if progress != nil {
				progress(written, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			_ = f.Close()
			_ = os.Remove(partial)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(partial)
		return err
	}

	// After download (before rename): verify digest when known.
	if err := VerifySHA256(partial, wantSHA); err != nil {
		return err
	}

	// remove tiny placeholders
	for _, name := range []string{"disk.img", "disk.qcow2", "disk.raw"} {
		p := filepath.Join(dir, name)
		if st, err := os.Stat(p); err == nil && st.Size() < 1024*1024 {
			_ = os.Remove(p)
		}
	}
	if err := os.Rename(partial, dest); err != nil {
		return err
	}
	m.writeMeta(id, spec, spec.HasAgent)
	return nil
}

func (m *Manager) client() *http.Client {
	if m.Client != nil {
		return m.Client
	}
	return http.DefaultClient
}

// resolveWantSHA256 returns the digest to verify after download.
// If pinned is non-empty it is used as-is. Otherwise, when imageURL is set,
// fetch imageURL+".sha256" and parse the first 64-hex token (sha256sum format).
//
// Missing sidecar or empty pin → empty string (caller fail-closes unless
// Spec.AllowUnverified). Sidecar network/HTTP errors (other than 404) are
// returned so pulls do not silently skip integrity checks.
func resolveWantSHA256(ctx context.Context, client *http.Client, imageURL, pinned string) (string, error) {
	if pinned != "" {
		return strings.TrimSpace(pinned), nil
	}
	if imageURL == "" {
		return "", nil
	}
	if client == nil {
		client = http.DefaultClient
	}
	sidecarURL := imageURL + ".sha256"
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, sidecarURL, nil)
	if err != nil {
		return "", err
	}
	res, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("sha256 sidecar: %w", err)
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		// No sidecar published — empty want; pullSpec fail-closes unless AllowUnverified.
		return "", nil
	}
	if res.StatusCode != http.StatusOK {
		return "", fmt.Errorf("sha256 sidecar: HTTP %s", res.Status)
	}
	body, err := io.ReadAll(io.LimitReader(res.Body, 4096))
	if err != nil {
		return "", fmt.Errorf("sha256 sidecar: read: %w", err)
	}
	digest := ParseSHA256Sidecar(string(body))
	if digest == "" {
		return "", fmt.Errorf("sha256 sidecar: no valid digest in body")
	}
	return digest, nil
}

// ParseSHA256Sidecar extracts a 64-char hex digest from sha256sum-style output:
//
//	"<hex>  filename" or bare "<hex>".
func ParseSHA256Sidecar(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	// First field is the digest (sha256sum / shasum -a 256).
	field := body
	if i := strings.IndexAny(body, " \t"); i >= 0 {
		field = body[:i]
	}
	field = strings.TrimSpace(field)
	if len(field) != 64 {
		return ""
	}
	for _, c := range field {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return ""
		}
	}
	return strings.ToLower(field)
}

// Import registers a local disk image as catalog id under images/<id>/disk.qcow2.
// srcPath may be qcow2, raw, or img; when qemu-img is available non-qcow2 sources
// are converted. Overwrites an existing local base for id.
//
// For grain-ubuntu (or any Spec.HasAgent), metadata has_agent=true is written.
// Unknown ids are rejected; use a catalog entry.
func (m *Manager) Import(ctx context.Context, id, srcPath string) error {
	if id == "" {
		return fmt.Errorf("import: empty image id")
	}
	if srcPath == "" {
		return fmt.Errorf("import: empty source path")
	}
	spec, err := Get(id)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(srcPath)
	if err != nil {
		return fmt.Errorf("import: %w", err)
	}
	st, err := os.Stat(abs)
	if err != nil {
		return fmt.Errorf("import: source: %w", err)
	}
	if st.IsDir() {
		return fmt.Errorf("import: source is a directory: %s", abs)
	}
	if st.Size() < 1024*1024 {
		return fmt.Errorf("import: source too small (%d bytes): %s", st.Size(), abs)
	}

	dir := m.Dir(id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	dest := filepath.Join(dir, "disk.qcow2")
	// Avoid copying onto itself when re-importing from the same path.
	if sameFile(abs, dest) {
		m.writeMeta(id, spec, hasAgentForImport(spec, id))
		return nil
	}

	// Remove previous base disks so Ready/DiskPath see the new one.
	for _, name := range []string{"disk.qcow2", "disk.img", "disk.raw"} {
		_ = os.Remove(filepath.Join(dir, name))
	}

	partial := dest + ".partial"
	_ = os.Remove(partial)

	if err := materializeQcow2(ctx, abs, partial); err != nil {
		_ = os.Remove(partial)
		return err
	}
	if err := os.Rename(partial, dest); err != nil {
		_ = os.Remove(partial)
		return err
	}

	// Record provenance
	_ = os.WriteFile(filepath.Join(dir, "source.import"), []byte(abs+"\n"), 0o644)
	m.writeMeta(id, spec, hasAgentForImport(spec, id))
	return nil
}

func hasAgentForImport(spec Spec, id string) bool {
	if spec.HasAgent {
		return true
	}
	// Explicit golden id always marks agent baked even if catalog drifts.
	return id == IDGrainUbuntu
}

// materializeQcow2 copies or converts src into dest as qcow2.
func materializeQcow2(ctx context.Context, src, dest string) error {
	ext := strings.ToLower(filepath.Ext(src))
	// Prefer qemu-img convert to flatten overlay chains into a standalone base.
	if qemuImg, err := hostbin.LookPath("qemu-img"); err == nil {
		cmd := exec.CommandContext(ctx, qemuImg, "convert", "-O", "qcow2", src, dest)
		if out, err := cmd.CombinedOutput(); err != nil {
			// Fall back to plain copy for already-standalone qcow2 if convert fails.
			if ext == ".qcow2" {
				if cerr := copyFile(src, dest); cerr == nil {
					return nil
				}
			}
			return fmt.Errorf("qemu-img convert: %w (%s)", err, strings.TrimSpace(string(out)))
		}
		return nil
	}

	// No qemu-img: only allow direct copy of qcow2-looking sources.
	if ext != ".qcow2" && ext != ".img" {
		return fmt.Errorf("import: qemu-img not found; need it to convert %s (brew install qemu)", ext)
	}
	return copyFile(src, dest)
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer func() { _ = in.Close() }()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer func() { _ = out.Close() }()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

func sameFile(a, b string) bool {
	ai, err1 := os.Stat(a)
	bi, err2 := os.Stat(b)
	if err1 != nil || err2 != nil {
		return false
	}
	return os.SameFile(ai, bi)
}

// ListLocal returns ids that appear pulled.
func (m *Manager) ListLocal() ([]string, error) {
	root := filepath.Join(m.DataDir, "images")
	entries, err := os.ReadDir(root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() && m.Ready(e.Name()) {
			out = append(out, e.Name())
		}
	}
	return out, nil
}
