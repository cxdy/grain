package image

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
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

// Pull downloads the image if missing. progress is optional (bytes written, total hint).
func (m *Manager) Pull(ctx context.Context, id string, progress func(written, total int64)) error {
	spec, err := Get(id)
	if err != nil {
		return err
	}
	if spec.URL == "" {
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

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, spec.URL, nil)
	if err != nil {
		return err
	}
	res, err := m.Client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("download: HTTP %s", res.Status)
	}

	f, err := os.Create(partial)
	if err != nil {
		return err
	}
	defer f.Close()

	total := res.ContentLength
	if total <= 0 {
		total = spec.SizeHint
	}
	var written int64
	buf := make([]byte, 256*1024)
	h := sha256.New()
	for {
		n, rerr := res.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				return err
			}
			_, _ = h.Write(buf[:n])
			written += int64(n)
			if progress != nil {
				progress(written, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}

	if spec.SHA256 != "" {
		sum := hex.EncodeToString(h.Sum(nil))
		if sum != spec.SHA256 {
			_ = os.Remove(partial)
			return fmt.Errorf("sha256 mismatch: got %s want %s", sum, spec.SHA256)
		}
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
	_ = os.WriteFile(filepath.Join(dir, "source.url"), []byte(spec.URL+"\n"), 0o644)
	_ = os.WriteFile(filepath.Join(dir, "ssh_user"), []byte(spec.SSHUser+"\n"), 0o644)
	return nil
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
