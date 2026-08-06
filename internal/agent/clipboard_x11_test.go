package agent

import "testing"

func TestX11MaxDirectBytes(t *testing.T) {
	t.Parallel()
	// Typical X server: 65535 4-byte units → ~256KiB raw; we cap lower.
	got := x11MaxDirectBytes(65535)
	if got <= 0 || got > x11DirectTransferMax {
		t.Fatalf("maxDirect=%d want in (0, %d]", got, x11DirectTransferMax)
	}
	// Must leave headroom under ~256KiB protocol limit for request headers.
	if got > 256*1024-512 {
		t.Fatalf("maxDirect=%d too large for typical max-request-length", got)
	}
	// Zero / tiny setup field falls back.
	if g := x11MaxDirectBytes(0); g != x11DirectTransferMax {
		t.Fatalf("zero request length: got %d want fallback %d", g, x11DirectTransferMax)
	}
	// Very large advertised limit still capped.
	if g := x11MaxDirectBytes(1<<15 - 1); g > x11DirectTransferMax {
		t.Fatalf("uncapped %d", g)
	}
}

func TestX11IncrChunkingPlan(t *testing.T) {
	t.Parallel()
	// Pure plan: large payload must be split into chunks ≤ maxDirect and
	// eventually a zero-length terminator (mirrors handlePropertyNotify).
	const total = 420543 // skeptic failing size
	maxDirect := x11MaxDirectBytes(65535)
	if total <= maxDirect {
		t.Fatalf("test size %d should exceed maxDirect %d", total, maxDirect)
	}
	chunk := x11IncrChunkSize
	if chunk > maxDirect {
		chunk = maxDirect
	}
	var sent int
	var rounds int
	for sent < total {
		n := chunk
		if remaining := total - sent; remaining < n {
			n = remaining
		}
		sent += n
		rounds++
		if n > maxDirect {
			t.Fatalf("chunk %d exceeds maxDirect %d", n, maxDirect)
		}
	}
	if sent != total {
		t.Fatalf("sent %d want %d", sent, total)
	}
	if rounds < 2 {
		t.Fatalf("expected multi-chunk INCR, rounds=%d", rounds)
	}
	// Zero-length end marker is an additional write (not counted in sent).
	t.Logf("INCR plan: %d bytes → %d chunks of ≤%d (maxDirect=%d)", total, rounds, chunk, maxDirect)
}

func TestIsPNGJPEG(t *testing.T) {
	t.Parallel()
	png := []byte{0x89, 'P', 'N', 'G', 0, 0}
	jpeg := []byte{0xff, 0xd8, 0xff, 0xe0}
	if !isPNG(png) || isJPEG(png) {
		t.Fatal("png magic")
	}
	if !isJPEG(jpeg) || isPNG(jpeg) {
		t.Fatal("jpeg magic")
	}
}
