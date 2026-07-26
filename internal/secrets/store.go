// Package secrets is a simple file-backed host secret store under DataDir/secrets.
package secrets

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"time"
)

// safeName matches secret identifiers (same style as VM names, plus underscore).
var safeName = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9._-]{0,62}$`)

// ValidName reports whether name is a safe secret identifier.
func ValidName(name string) bool {
	return safeName.MatchString(name)
}

// Meta is the on-disk metadata for a secret (data stored separately).
type Meta struct {
	Name      string    `json:"name"`
	Mode      string    `json:"mode,omitempty"` // octal, default 0600 when materialised
	UID       *uint32   `json:"uid,omitempty"`
	GID       *uint32   `json:"gid,omitempty"`
	Size      int64     `json:"size"`
	UpdatedAt time.Time `json:"updated_at"`
}

// Secret is Meta plus optional base64 payload (included on GET/POST).
type Secret struct {
	Meta
	DataBase64 string `json:"data_base64,omitempty"`
}

// PutRequest is the body for create/update.
type PutRequest struct {
	Name       string  `json:"name"`
	DataBase64 string  `json:"data_base64"`
	Mode       string  `json:"mode,omitempty"`
	UID        *uint32 `json:"uid,omitempty"`
	GID        *uint32 `json:"gid,omitempty"`
}

// Store persists secrets under baseDir/<name>/{meta.json,data}.
type Store struct {
	mu  sync.RWMutex
	dir string
}

// New returns a Store rooted at dataDir/secrets (creates the directory).
func New(dataDir string) (*Store, error) {
	dir := filepath.Join(dataDir, "secrets")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Dir returns the secrets root directory.
func (s *Store) Dir() string { return s.dir }

func (s *Store) secretDir(name string) string {
	return filepath.Join(s.dir, name)
}

func (s *Store) metaPath(name string) string {
	return filepath.Join(s.secretDir(name), "meta.json")
}

func (s *Store) dataPath(name string) string {
	return filepath.Join(s.secretDir(name), "data")
}

// List returns metadata for all secrets (no data_base64).
func (s *Store) List() ([]Meta, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	entries, err := os.ReadDir(s.dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []Meta
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		m, err := s.readMetaUnlocked(e.Name())
		if err != nil {
			continue
		}
		out = append(out, m)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

// Get returns a secret with data_base64. includeData=false omits payload.
func (s *Store) Get(name string, includeData bool) (*Secret, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid secret name %q", name)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	m, err := s.readMetaUnlocked(name)
	if err != nil {
		return nil, err
	}
	sec := &Secret{Meta: m}
	if includeData {
		b, err := os.ReadFile(s.dataPath(name))
		if err != nil {
			return nil, err
		}
		sec.DataBase64 = base64.StdEncoding.EncodeToString(b)
	}
	return sec, nil
}

// Put creates or replaces a secret. Mode defaults to "0600".
func (s *Store) Put(req PutRequest) (*Meta, error) {
	if !ValidName(req.Name) {
		return nil, fmt.Errorf("invalid secret name %q", req.Name)
	}
	if req.DataBase64 == "" {
		return nil, fmt.Errorf("data_base64 is required")
	}
	data, err := decodeB64(req.DataBase64)
	if err != nil {
		return nil, fmt.Errorf("invalid data_base64: %w", err)
	}
	mode := req.Mode
	if mode == "" {
		mode = "0600"
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	d := s.secretDir(req.Name)
	if err := os.MkdirAll(d, 0o700); err != nil {
		return nil, err
	}
	// Write data with 0600.
	tmpData := s.dataPath(req.Name) + ".tmp"
	if err := os.WriteFile(tmpData, data, 0o600); err != nil {
		return nil, err
	}
	if err := os.Rename(tmpData, s.dataPath(req.Name)); err != nil {
		_ = os.Remove(tmpData)
		return nil, err
	}
	// Ensure final data file is 0600 (umask may have altered Create).
	_ = os.Chmod(s.dataPath(req.Name), 0o600)

	m := Meta{
		Name:      req.Name,
		Mode:      mode,
		UID:       req.UID,
		GID:       req.GID,
		Size:      int64(len(data)),
		UpdatedAt: time.Now().UTC(),
	}
	if err := s.writeMetaUnlocked(m); err != nil {
		return nil, err
	}
	// Directory and files: 0700/0600.
	_ = os.Chmod(d, 0o700)
	return &m, nil
}

// Patch updates metadata and/or data for an existing secret.
func (s *Store) Patch(name string, req PutRequest) (*Meta, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid secret name %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	m, err := s.readMetaUnlocked(name)
	if err != nil {
		return nil, err
	}
	if req.DataBase64 != "" {
		data, err := decodeB64(req.DataBase64)
		if err != nil {
			return nil, fmt.Errorf("invalid data_base64: %w", err)
		}
		tmpData := s.dataPath(name) + ".tmp"
		if err := os.WriteFile(tmpData, data, 0o600); err != nil {
			return nil, err
		}
		if err := os.Rename(tmpData, s.dataPath(name)); err != nil {
			_ = os.Remove(tmpData)
			return nil, err
		}
		_ = os.Chmod(s.dataPath(name), 0o600)
		m.Size = int64(len(data))
	}
	if req.Mode != "" {
		m.Mode = req.Mode
	}
	if req.UID != nil {
		m.UID = req.UID
	}
	if req.GID != nil {
		m.GID = req.GID
	}
	m.UpdatedAt = time.Now().UTC()
	if err := s.writeMetaUnlocked(m); err != nil {
		return nil, err
	}
	return &m, nil
}

// Delete removes a secret.
func (s *Store) Delete(name string) error {
	if !ValidName(name) {
		return fmt.Errorf("invalid secret name %q", name)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	d := s.secretDir(name)
	if _, err := os.Stat(d); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("secret %q not found", name)
		}
		return err
	}
	return os.RemoveAll(d)
}

// ReadData returns raw secret bytes.
func (s *Store) ReadData(name string) ([]byte, error) {
	if !ValidName(name) {
		return nil, fmt.Errorf("invalid secret name %q", name)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, err := s.readMetaUnlocked(name); err != nil {
		return nil, err
	}
	return os.ReadFile(s.dataPath(name))
}

func (s *Store) readMetaUnlocked(name string) (Meta, error) {
	b, err := os.ReadFile(s.metaPath(name))
	if err != nil {
		if os.IsNotExist(err) {
			return Meta{}, fmt.Errorf("secret %q not found", name)
		}
		return Meta{}, err
	}
	var m Meta
	if err := json.Unmarshal(b, &m); err != nil {
		return Meta{}, err
	}
	if m.Name == "" {
		m.Name = name
	}
	return m, nil
}

func (s *Store) writeMetaUnlocked(m Meta) error {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.metaPath(m.Name) + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.metaPath(m.Name))
}

func decodeB64(s string) ([]byte, error) {
	if b, err := base64.StdEncoding.DecodeString(s); err == nil {
		return b, nil
	}
	return base64.RawStdEncoding.DecodeString(s)
}
