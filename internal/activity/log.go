// Package activity is an in-memory ring of recent control-plane actions
// (create/start/stop/rm/exec/…) for Desktop and operators.
package activity

import (
	"sync"
	"sync/atomic"
	"time"
)

// DefaultCapacity is how many events to retain.
const DefaultCapacity = 200

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
}

// New returns a log with the given capacity (minimum 16).
func New(capacity int) *Log {
	if capacity < 16 {
		capacity = DefaultCapacity
	}
	return &Log{cap: capacity, ring: make([]Event, 0, capacity)}
}

// Record appends an event (assigns id and time if empty).
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
	return ev
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
