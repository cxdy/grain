package vmsync

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// syncStateVersion is the on-disk schema version for DataDir/sync/*.json.
const syncStateVersion = 1

// syncFingerprint is one side (host or guest) of a baselined path.
// Content equality uses type + size + mtime (mode is applied separately).
type syncFingerprint struct {
	Type   string `json:"type"` // file | directory | symlink
	Size   int64  `json:"size"`
	Mtime  int64  `json:"mtime"` // unix seconds
	Mode   string `json:"mode,omitempty"`
	Target string `json:"target,omitempty"` // symlink target
	SHA256 string `json:"sha256,omitempty"`
}

// syncEntry holds direction-independent baselines for one relative path.
type syncEntry struct {
	Host  *syncFingerprint `json:"host,omitempty"`
	Guest *syncFingerprint `json:"guest,omitempty"`
}

// complete reports whether both sides are recorded (required for three-way).
func (e syncEntry) complete() bool {
	return e.Host != nil && e.Guest != nil
}

// syncState is the host-side baseline file for one (api, vm, hostRoot, guestRoot) pair.
type syncState struct {
	Version     int                  `json:"version"`
	VM          string               `json:"vm"`
	HostRoot    string               `json:"host_root"`
	GuestRoot   string               `json:"guest_root"`
	API         string               `json:"api"`
	UpdatedAt   string               `json:"updated_at"`
	Fingerprint string               `json:"fingerprint"` // size_mtime | checksum
	Entries     map[string]syncEntry `json:"entries"`
}

// newSyncState constructs an empty state document.
func newSyncState(api, vm, hostRoot, guestRoot string) *syncState {
	return &syncState{
		Version:     syncStateVersion,
		VM:          vm,
		HostRoot:    hostRoot,
		GuestRoot:   guestRoot,
		API:         api,
		Fingerprint: "size_mtime",
		Entries:     make(map[string]syncEntry),
	}
}

// syncStateID returns a stable 32-hex identity with no push/pull direction.
//
//	id = hex(sha256(apiIdentity + "\0" + vm + "\0" + absHostRoot + "\0" + guestRoot))[:32]
func syncStateID(apiIdentity, vm, absHostRoot, guestRoot string) string {
	h := sha256.New()
	_, _ = fmt.Fprintf(h, "%s\x00%s\x00%s\x00%s", apiIdentity, vm, absHostRoot, guestRoot)
	sum := hex.EncodeToString(h.Sum(nil))
	if len(sum) > 32 {
		return sum[:32]
	}
	return sum
}

// syncStatePath is cfg.DataDir/sync/<id>.json.
func syncStatePath(dataDir, id string) string {
	return filepath.Join(dataDir, "sync", id+".json")
}

// loadSyncState reads a state file. Missing file returns (nil, nil).
func loadSyncState(path string) (*syncState, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var st syncState
	if err := json.Unmarshal(b, &st); err != nil {
		return nil, fmt.Errorf("sync state: %w", err)
	}
	if st.Entries == nil {
		st.Entries = make(map[string]syncEntry)
	}
	if st.Version == 0 {
		st.Version = syncStateVersion
	}
	return &st, nil
}

// saveSyncState writes state atomically (temp + rename) with mode 0600.
// Parent directory is created at 0700.
func saveSyncState(path string, st *syncState) error {
	if st == nil {
		return fmt.Errorf("sync state: nil")
	}
	if st.Entries == nil {
		st.Entries = make(map[string]syncEntry)
	}
	st.Version = syncStateVersion
	st.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
	if st.Fingerprint == "" {
		st.Fingerprint = "size_mtime"
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')

	tmp, err := os.CreateTemp(dir, ".sync-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(b); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	ok = true
	return nil
}

// entry returns a complete baseline for relPath, or false if missing/incomplete.
func (st *syncState) entry(relPath string) (syncEntry, bool) {
	if st == nil || st.Entries == nil {
		return syncEntry{}, false
	}
	e, ok := st.Entries[relPath]
	if !ok || !e.complete() {
		return syncEntry{}, false
	}
	return e, true
}

// setEntry stores both sides for relPath.
func (st *syncState) setEntry(relPath string, host, guest *syncFingerprint) {
	if st.Entries == nil {
		st.Entries = make(map[string]syncEntry)
	}
	st.Entries[relPath] = syncEntry{Host: host, Guest: guest}
}

// removeEntry drops a path after --delete.
func (st *syncState) removeEntry(relPath string) {
	if st == nil || st.Entries == nil {
		return
	}
	delete(st.Entries, relPath)
}

// fpContentEqual compares content fingerprints (type+size+mtime for files; type for dirs).
// Mode and sha256 are ignored.
func fpContentEqual(a, b *syncFingerprint) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case "directory":
		return true
	case "symlink":
		// Content identity is the link target string.
		return a.Target == b.Target
	default:
		return a.Size == b.Size && a.Mtime == b.Mtime
	}
}

// invToFingerprint converts an inventory entry to a baseline fingerprint.
func invToFingerprint(e *syncInvEntry) *syncFingerprint {
	if e == nil {
		return nil
	}
	return &syncFingerprint{
		Type:   e.Type,
		Size:   e.Size,
		Mtime:  e.Mtime,
		Mode:   normalizeSyncMode(e.Mode),
		Target: e.Target,
	}
}

// normalizeSyncMode trims and lowercases optional leading zeros form (0644).
func normalizeSyncMode(mode string) string {
	mode = strings.TrimSpace(mode)
	if mode == "" {
		return ""
	}
	// Keep octal string as stored; strip "0o" prefix if present.
	mode = strings.TrimPrefix(mode, "0o")
	mode = strings.TrimPrefix(mode, "0O")
	return mode
}

// modesEqual compares normalized mode strings (empty matches empty).
func modesEqual(a, b string) bool {
	return normalizeSyncMode(a) == normalizeSyncMode(b)
}
