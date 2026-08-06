// Package activity is a ring of recent control-plane actions
// (create/start/stop/rm/exec/…) for Desktop and operators.
// Optionally persisted under data_dir/activity.json across daemon restarts.
package activity

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCapacity is how many events to retain.
const DefaultCapacity = 200

// FileName is the default log file under the grain data dir.
const FileName = "activity.json"

// Event is one recorded API action.
type Event struct {
	ID         string `json:"id"`
	Time       string `json:"t"` // RFC3339
	Action     string `json:"action"`
	Target     string `json:"target,omitempty"`
	Source     string `json:"source,omitempty"` // cli|desktop|mcp|sdk|api
	Status     string `json:"status"`           // success|error
	DurationMS int64  `json:"duration_ms,omitempty"`
	Summary    string `json:"summary,omitempty"`
	Detail     string `json:"detail,omitempty"`
	Method     string `json:"method,omitempty"`
	Path       string `json:"path,omitempty"`
}

// Log is a concurrency-safe ring buffer of Events (newest first on List).
type Log struct {
	mu   sync.Mutex
	cap  int
	seq  atomic.Uint64
	ring []Event
	path string // empty = memory only
}

// New returns an in-memory log with the given capacity (minimum 16).
func New(capacity int) *Log {
	if capacity < 16 {
		capacity = DefaultCapacity
	}
	return &Log{cap: capacity, ring: make([]Event, 0, capacity)}
}

// Open loads or creates a persistent log at path (JSON array, newest first).
// path empty falls back to New(capacity).
func Open(path string, capacity int) (*Log, error) {
	l := New(capacity)
	path = filepath.Clean(strings.TrimSpace(path))
	if path == "" || path == "." {
		return l, nil
	}
	l.path = path
	if err := l.load(); err != nil && !os.IsNotExist(err) {
		// Corrupt file: start fresh but keep path for future writes.
		l.ring = l.ring[:0]
	}
	return l, nil
}

// PathForDataDir returns dataDir/activity.json.
func PathForDataDir(dataDir string) string {
	if dataDir == "" {
		return ""
	}
	return filepath.Join(dataDir, FileName)
}

// Record appends an event (assigns id and time if empty) and persists when path is set.
func (l *Log) Record(ev Event) Event {
	if l == nil {
		return ev
	}
	if ev.Time == "" {
		ev.Time = time.Now().UTC().Format(time.RFC3339Nano)
	}
	if ev.ID == "" {
		n := l.seq.Add(1)
		ev.ID = "act-" + itoa(n)
	}
	if ev.Status == "" {
		ev.Status = "success"
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.ring) >= l.cap {
		// drop oldest (end of slice when newest-first)
		l.ring = l.ring[:len(l.ring)-1]
	}
	// newest first
	l.ring = append([]Event{ev}, l.ring...)
	_ = l.persistLocked()
	return ev
}

type diskSnapshot struct {
	Seq    uint64  `json:"seq"`
	Events []Event `json:"events"`
}

func (l *Log) load() error {
	b, err := os.ReadFile(l.path)
	if err != nil {
		return err
	}
	var snap diskSnapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		// try bare array for forward-compat
		var arr []Event
		if err2 := json.Unmarshal(b, &arr); err2 != nil {
			return err
		}
		snap.Events = arr
	}
	if len(snap.Events) > l.cap {
		snap.Events = snap.Events[:l.cap]
	}
	l.ring = snap.Events
	if snap.Seq > 0 {
		l.seq.Store(snap.Seq)
	} else {
		// infer seq from highest act-N id
		var max uint64
		for _, e := range l.ring {
			if n, ok := parseActID(e.ID); ok && n > max {
				max = n
			}
		}
		l.seq.Store(max)
	}
	return nil
}

func (l *Log) persistLocked() error {
	if l.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(l.path), 0o700); err != nil {
		return err
	}
	snap := diskSnapshot{Seq: l.seq.Load(), Events: l.ring}
	b, err := json.MarshalIndent(snap, "", "  ")
	if err != nil {
		return err
	}
	tmp := l.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, l.path)
}

func parseActID(id string) (uint64, bool) {
	const p = "act-"
	if len(id) <= len(p) || id[:len(p)] != p {
		return 0, false
	}
	var n uint64
	for i := len(p); i < len(id); i++ {
		c := id[i]
		if c < '0' || c > '9' {
			return 0, false
		}
		n = n*10 + uint64(c-'0')
	}
	return n, true
}

// List returns up to limit newest events (0 = all retained).
func (l *Log) List(limit int) []Event {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > len(l.ring) {
		limit = len(l.ring)
	}
	out := make([]Event, limit)
	copy(out, l.ring[:limit])
	return out
}

// ListSince returns events newer than afterID (exclusive), newest first.
// If afterID is empty or unknown, returns List(limit).
func (l *Log) ListSince(afterID string, limit int) []Event {
	all := l.List(0)
	if afterID == "" {
		if limit > 0 && len(all) > limit {
			return all[:limit]
		}
		return all
	}
	var out []Event
	for _, e := range all {
		if e.ID == afterID {
			break
		}
		out = append(out, e)
	}
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func itoa(n uint64) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
