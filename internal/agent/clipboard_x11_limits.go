package agent

// X11 CLIPBOARD transfer sizing (ICCCM INCR). Pure helpers live here so
// unit tests run on every GOOS; the X11 event loop is linux-only.

// x11DirectTransferMax is a conservative upper bound for a single
// ChangeProperty request. The X protocol max-request-length is often ~256KiB
// of data; larger payloads must use ICCCM INCR or clients hang / get 0 bytes.
const x11DirectTransferMax = 240 * 1024 // 240 KiB of property data

// x11IncrChunkSize is the per-chunk size for INCR transfers (well under the
// max request length after protocol headers).
const x11IncrChunkSize = 64 * 1024

// x11MaxDirectBytes returns the largest property payload we may send in one
// ChangeProperty request given the connection's MaximumRequestLength (in
// 4-byte units). Leaves headroom for request headers.
func x11MaxDirectBytes(maxRequestLength uint16) int {
	// Setup field is in 4-byte units; convert to bytes and reserve ~1KiB header.
	limit := int(maxRequestLength)*4 - 1024
	if limit <= 0 {
		return x11DirectTransferMax
	}
	if limit > x11DirectTransferMax {
		return x11DirectTransferMax
	}
	// Never go below a small but useful direct size.
	if limit < 4096 {
		return 4096
	}
	return limit
}

func isPNG(b []byte) bool {
	return len(b) >= 4 && b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G'
}

func isJPEG(b []byte) bool {
	return len(b) >= 2 && b[0] == 0xff && b[1] == 0xd8
}
