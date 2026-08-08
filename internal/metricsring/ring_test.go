package metricsring

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestRingAppendRead(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "metrics.ring")
	r, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	for i := 0; i < 10; i++ {
		if err := r.Append(Sample{
			TimeMS:   time.Now().UnixMilli() + int64(i),
			Load1:    float64(i) * 0.1,
			MemTotal: 1000,
			MemAvail: uint64(900 - i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	all, err := r.ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	if len(all) != 8 {
		t.Fatalf("len %d want 8 (capacity)", len(all))
	}
	// Oldest should be samples 2..9 (first two overwritten)
	if all[0].Load1 < 0.15 {
		t.Fatalf("oldest load %v", all[0].Load1)
	}
	// Reopen
	_ = r.Close()
	r2, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	all2, err := r2.ReadAll()
	if err != nil || len(all2) != 8 {
		t.Fatalf("%d %v", len(all2), err)
	}
}

func TestRingCapacityClampAndPath(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// capacity < 4 → 4
	r, err := Open(filepath.Join(dir, "small.ring"), 1)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.capacity != 4 {
		t.Fatalf("cap %d", r.capacity)
	}
	// Upper clamp: capacity > 200_000 → 200_000. Skip full 12MB prealloc; the
	// assignment is exercised by Open with a value just over the max and a
	// modest capacity file already covers create path. Use 5 to stay light.
	r2, err := Open(filepath.Join(dir, "n.ring"), 5)
	if err != nil {
		t.Fatal(err)
	}
	if r2.capacity != 5 {
		t.Fatalf("cap5 %d", r2.capacity)
	}
	_ = r2.Close()

	if Path(dir) != filepath.Join(dir, "metrics.ring") {
		t.Fatal(Path(dir))
	}
	all, err := r.ReadAll()
	if err != nil || all != nil {
		t.Fatalf("empty: %v %v", all, err)
	}
	// Auto TimeMS
	if err := r.Append(Sample{Load1: 1.5, MemTotal: 10}); err != nil {
		t.Fatal(err)
	}
	all, err = r.ReadAll()
	if err != nil || len(all) != 1 || all[0].TimeMS == 0 || all[0].Load1 != 1.5 {
		t.Fatalf("%+v %v", all, err)
	}
}

func TestRingClosedErrorsAndDoubleClose(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "c.ring")
	r, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err) // second close is nil
	}
	if err := r.Append(Sample{TimeMS: 1}); err == nil {
		t.Fatal("append closed")
	}
	if _, err := r.ReadAll(); err == nil {
		t.Fatal("read closed")
	}
}

func TestRingRecreateOnBadHeaderOrCapacityChange(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "m.ring")
	r, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := r.Append(Sample{TimeMS: int64(100 + i), Load1: float64(i)}); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Close()

	// capacity change recreates
	r2, err := Open(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	all, err := r2.ReadAll()
	if err != nil || len(all) != 0 {
		t.Fatalf("recreate empty: %d %v", len(all), err)
	}
	if err := r2.Append(Sample{TimeMS: 1, Load1: 9}); err != nil {
		t.Fatal(err)
	}
	_ = r2.Close()

	// corrupt magic → recreate
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var hdr [16]byte
	binary.LittleEndian.PutUint32(hdr[0:4], 0xdeadbeef)
	binary.LittleEndian.PutUint32(hdr[4:8], version)
	binary.LittleEndian.PutUint32(hdr[8:12], 16)
	binary.LittleEndian.PutUint32(hdr[12:16], 0)
	if _, err := f.WriteAt(hdr[:], 0); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()

	r3, err := Open(path, 16)
	if err != nil {
		t.Fatal(err)
	}
	defer r3.Close()
	all, err = r3.ReadAll()
	if err != nil || len(all) != 0 {
		t.Fatalf("bad magic recreate: %d %v", len(all), err)
	}
}

func TestRingPartialFillReopen(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "p.ring")
	r, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 3; i++ {
		if err := r.Append(Sample{
			TimeMS:     int64(1000 + i),
			Load1:      float64(i) + 0.25,
			MemTotal:   100,
			MemAvail:   50,
			DiskTotal:  200,
			DiskFree:   100,
			NetRxBytes: 1,
			NetTxBytes: 2,
		}); err != nil {
			t.Fatal(err)
		}
	}
	_ = r.Close()
	r2, err := Open(path, 10)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	all, err := r2.ReadAll()
	if err != nil || len(all) != 3 {
		t.Fatalf("%d %v", len(all), err)
	}
	if all[0].TimeMS != 1000 || all[2].Load1 != 2.25 {
		t.Fatalf("%+v", all)
	}
	// encode/decode round-trip fields
	if all[0].MemTotal != 100 || all[0].NetTxBytes != 2 {
		t.Fatalf("%+v", all[0])
	}
}

func TestRingOpenNestedDir(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "a", "b", "metrics.ring")
	r, err := Open(path, 4)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if err := r.Append(Sample{TimeMS: 42}); err != nil {
		t.Fatal(err)
	}
}

func TestEncodeDecodeSample(t *testing.T) {
	t.Parallel()
	s := Sample{
		TimeMS: 99, Load1: 1.25, MemTotal: 1, MemAvail: 2,
		DiskTotal: 3, DiskFree: 4, NetRxBytes: 5, NetTxBytes: 6,
	}
	b := encodeSample(s)
	got := decodeSample(b[:])
	if got != s {
		t.Fatalf("%+v vs %+v", got, s)
	}
}

func TestRingOpenMkdirFails(t *testing.T) {
	t.Parallel()
	base := t.TempDir()
	// parent path is a file → MkdirAll fails
	file := filepath.Join(base, "notadir")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(filepath.Join(file, "metrics.ring"), 8); err == nil {
		t.Fatal("expected mkdir error")
	}
}

func TestRingBadVersionRecreate(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "v.ring")
	r, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Append(Sample{TimeMS: 1, Load1: 1}); err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
	// corrupt version
	f, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	var hdr [16]byte
	if _, err := f.ReadAt(hdr[:], 0); err != nil {
		t.Fatal(err)
	}
	binary.LittleEndian.PutUint32(hdr[4:8], 99) // bad version
	if _, err := f.WriteAt(hdr[:], 0); err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	r2, err := Open(path, 8)
	if err != nil {
		t.Fatal(err)
	}
	defer r2.Close()
	all, err := r2.ReadAll()
	if err != nil || len(all) != 0 {
		t.Fatalf("recreate after bad version: %d %v", len(all), err)
	}
}

func TestRingCapacityUpperClamp(t *testing.T) {
	// Allocates ~12.8MB; serial to avoid parallel disk pressure.
	path := filepath.Join(t.TempDir(), "big.ring")
	r, err := Open(path, 250_000)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	if r.capacity != 200_000 {
		t.Fatalf("want 200000 got %d", r.capacity)
	}
}
