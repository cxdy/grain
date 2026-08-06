// Package metricsring is a fixed-capacity on-disk ring of sandbox stats samples.
// Stored on the grain host under data_dir/vms/<name>/metrics.ring so Desktop
// (local or remote) always reads history from the daemon that owns the VM.
package metricsring

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	magic   = 0x47524e4d // "GRNM"
	version = 1
	// header: magic u32, version u32, capacity u32, writeIdx u32 = 16 bytes
	headerSize = 16
	// sample: ts_ms i64 + 7×u64/float64 fields = 8 + 56 = 64
	sampleSize = 64
)

// Sample is one guest stats snapshot stored in the ring.
type Sample struct {
	TimeMS     int64   `json:"t_ms"`
	Load1      float64 `json:"load1"`
	MemTotal   uint64  `json:"mem_total_bytes"`
	MemAvail   uint64  `json:"mem_available_bytes"`
	DiskTotal  uint64  `json:"disk_total_bytes"`
	DiskFree   uint64  `json:"disk_free_bytes"`
	NetRxBytes uint64  `json:"net_rx_bytes"`
	NetTxBytes uint64  `json:"net_tx_bytes"`
}

// Ring is a thread-safe fixed-capacity metrics ring file.
type Ring struct {
	mu       sync.Mutex
	path     string
	capacity int
	writeIdx int // next slot to write (0..capacity-1), wraps
	count    int // how many slots filled (≤ capacity)
	f        *os.File
}

// DefaultCapacity is ~24h at 15s interval.
const DefaultCapacity = 5760

// Open creates or opens path with the given capacity (slots). If the file exists
// with a different capacity, it is recreated.
func Open(path string, capacity int) (*Ring, error) {
	if capacity < 4 {
		capacity = 4
	}
	if capacity > 200_000 {
		capacity = 200_000
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	r := &Ring{path: path, capacity: capacity}
	if err := r.openOrCreate(); err != nil {
		return nil, err
	}
	return r, nil
}

func (r *Ring) openOrCreate() error {
	f, err := os.OpenFile(r.path, os.O_RDWR, 0o600)
	if err == nil {
		var hdr [headerSize]byte
		if _, err := f.ReadAt(hdr[:], 0); err == nil {
			mag := binary.LittleEndian.Uint32(hdr[0:4])
			ver := binary.LittleEndian.Uint32(hdr[4:8])
			cap := int(binary.LittleEndian.Uint32(hdr[8:12]))
			widx := int(binary.LittleEndian.Uint32(hdr[12:16]))
			if mag == magic && ver == version && cap == r.capacity && widx >= 0 && widx < r.capacity {
				r.f = f
				r.writeIdx = widx
				// Persist fill count in a simple way: scan for non-zero timestamps.
				// Full preallocation means size is always capacity; infer from samples.
				n := 0
				for i := 0; i < r.capacity; i++ {
					off := int64(headerSize + i*sampleSize)
					var buf [8]byte
					if _, err := f.ReadAt(buf[:], off); err != nil {
						break
					}
					if binary.LittleEndian.Uint64(buf[:]) != 0 {
						n++
					}
				}
				r.count = n
				if r.count > r.capacity {
					r.count = r.capacity
				}
				return nil
			}
		}
		_ = f.Close()
		_ = os.Remove(r.path)
	}
	f, err = os.OpenFile(r.path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	// Preallocate full ring with zeros
	size := int64(headerSize + r.capacity*sampleSize)
	if err := f.Truncate(size); err != nil {
		_ = f.Close()
		return err
	}
	r.f = f
	r.writeIdx = 0
	r.count = 0
	return r.writeHeader()
}

func (r *Ring) writeHeader() error {
	var hdr [headerSize]byte
	binary.LittleEndian.PutUint32(hdr[0:4], magic)
	binary.LittleEndian.PutUint32(hdr[4:8], version)
	binary.LittleEndian.PutUint32(hdr[8:12], uint32(r.capacity))
	binary.LittleEndian.PutUint32(hdr[12:16], uint32(r.writeIdx))
	_, err := r.f.WriteAt(hdr[:], 0)
	return err
}

// Append writes a sample, overwriting the oldest when full.
func (r *Ring) Append(s Sample) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return fmt.Errorf("ring closed")
	}
	if s.TimeMS == 0 {
		s.TimeMS = time.Now().UnixMilli()
	}
	off := int64(headerSize + r.writeIdx*sampleSize)
	buf := encodeSample(s)
	if _, err := r.f.WriteAt(buf[:], off); err != nil {
		return err
	}
	r.writeIdx = (r.writeIdx + 1) % r.capacity
	if r.count < r.capacity {
		r.count++
	}
	return r.writeHeader()
}

// ReadAll returns samples oldest→newest (up to capacity).
func (r *Ring) ReadAll() ([]Sample, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil, fmt.Errorf("ring closed")
	}
	if r.count == 0 {
		return nil, nil
	}
	out := make([]Sample, 0, r.count)
	start := 0
	if r.count == r.capacity {
		start = r.writeIdx // oldest
	}
	for i := 0; i < r.count; i++ {
		idx := (start + i) % r.capacity
		off := int64(headerSize + idx*sampleSize)
		var buf [sampleSize]byte
		if _, err := r.f.ReadAt(buf[:], off); err != nil {
			return out, err
		}
		s := decodeSample(buf[:])
		if s.TimeMS == 0 {
			continue
		}
		out = append(out, s)
	}
	return out, nil
}

// Close flushes and closes the file.
func (r *Ring) Close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.f == nil {
		return nil
	}
	err := r.f.Close()
	r.f = nil
	return err
}

// Path returns the ring file path for a VM dir.
func Path(vmDir string) string {
	return filepath.Join(vmDir, "metrics.ring")
}

func encodeSample(s Sample) [sampleSize]byte {
	var b [sampleSize]byte
	binary.LittleEndian.PutUint64(b[0:8], uint64(s.TimeMS))
	binary.LittleEndian.PutUint64(b[8:16], math.Float64bits(s.Load1))
	binary.LittleEndian.PutUint64(b[16:24], s.MemTotal)
	binary.LittleEndian.PutUint64(b[24:32], s.MemAvail)
	binary.LittleEndian.PutUint64(b[32:40], s.DiskTotal)
	binary.LittleEndian.PutUint64(b[40:48], s.DiskFree)
	binary.LittleEndian.PutUint64(b[48:56], s.NetRxBytes)
	binary.LittleEndian.PutUint64(b[56:64], s.NetTxBytes)
	return b
}

func decodeSample(b []byte) Sample {
	return Sample{
		TimeMS:     int64(binary.LittleEndian.Uint64(b[0:8])),
		Load1:      math.Float64frombits(binary.LittleEndian.Uint64(b[8:16])),
		MemTotal:   binary.LittleEndian.Uint64(b[16:24]),
		MemAvail:   binary.LittleEndian.Uint64(b[24:32]),
		DiskTotal:  binary.LittleEndian.Uint64(b[32:40]),
		DiskFree:   binary.LittleEndian.Uint64(b[40:48]),
		NetRxBytes: binary.LittleEndian.Uint64(b[48:56]),
		NetTxBytes: binary.LittleEndian.Uint64(b[56:64]),
	}
}
