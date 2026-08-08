// Package osc52 intercepts OSC 52 clipboard escape sequences in a PTY byte
// stream and writes the decoded payload to the host clipboard.
//
// Interactive tools running inside a grain sandbox emit OSC 52 so copy can reach
// the user's terminal over SSH/PTY. Terminals without native OSC 52 support
// (notably Apple Terminal) never apply those sequences; grain sh therefore
// copies on the host side.
//
// Sequence forms handled:
//
//	ESC ] 52 ; <target> ; <base64> BEL
//	ESC ] 52 ; <target> ; <base64> ESC \
//	ESC P tmux ; ESC ESC ] 52 ; <target> ; <base64> BEL ESC \   (tmux DCS wrap)
//
// The original bytes are still forwarded to the underlying writer (pass-through)
// so terminals with native OSC 52 keep working. Set GRAIN_OSC52_PASSTHROUGH=0 to
// swallow intercepted sequences after copying. Set GRAIN_OSC52_CLIPBOARD=0 to
// disable interception entirely.
package osc52

import (
	"bytes"
	"encoding/base64"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
)

// Writer wraps an io.Writer and intercepts OSC 52 sequences for host clipboard.
type Writer struct {
	dst io.Writer

	mu     sync.Mutex
	buf    []byte // unparsed carry
	copyFn func([]byte) error
	// passthrough when true re-emits OSC 52 to dst after intercept.
	passthrough bool
}

// NewWriter returns a Writer that copies OSC 52 payloads to the host clipboard
// and writes all bytes (including the sequences) to dst.
func NewWriter(dst io.Writer) *Writer {
	return &Writer{
		dst:         dst,
		copyFn:      writeClipboard,
		passthrough: passthroughEnabled(),
	}
}

// Write implements io.Writer.
func (w *Writer) Write(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	w.mu.Lock()
	defer w.mu.Unlock()

	w.buf = append(w.buf, p...)
	if err := w.drain(); err != nil {
		// Report full acceptance of p if we consumed it into buf; mirror
		// bytes.Buffer semantics for the caller's accounting.
		return len(p), err
	}
	return len(p), nil
}

func (w *Writer) drain() error {
	for {
		// Find start of a candidate sequence: ESC ]
		// or ESC P (DCS, possibly tmux wrapper).
		i := indexOSCStart(w.buf)
		if i < 0 {
			// No start marker: flush everything except a trailing ESC
			// (might be incomplete start).
			if len(w.buf) > 0 && w.buf[len(w.buf)-1] == 0x1b {
				if err := w.emit(w.buf[:len(w.buf)-1]); err != nil {
					return err
				}
				w.buf = w.buf[len(w.buf)-1:]
			} else {
				if err := w.emit(w.buf); err != nil {
					return err
				}
				w.buf = w.buf[:0]
			}
			return nil
		}
		// Emit bytes before the sequence start.
		if i > 0 {
			if err := w.emit(w.buf[:i]); err != nil {
				return err
			}
			w.buf = w.buf[i:]
		}

		end, payload, ok := parseOSC52(w.buf)
		if !ok {
			// Incomplete OSC/DCS, or ESC P that is not tmux — wait if this
			// still looks like a partial prefix, otherwise emit one byte.
			if isPartialOSCPrefix(w.buf) && len(w.buf) <= maxOSCBuf {
				return nil
			}
			// Not a sequence we parse: emit first byte and rescan.
			if err := w.emit(w.buf[:1]); err != nil {
				return err
			}
			w.buf = w.buf[1:]
			continue
		}

		seq := w.buf[:end]
		if len(payload) > 0 && w.copyFn != nil {
			_ = w.copyFn(payload) // best-effort clipboard
		}
		if w.passthrough {
			if err := w.emit(seq); err != nil {
				return err
			}
		}
		w.buf = w.buf[end:]
	}
}

func (w *Writer) emit(p []byte) error {
	if len(p) == 0 {
		return nil
	}
	_, err := w.dst.Write(p)
	return err
}

const maxOSCBuf = 2 << 20 // 2 MiB incomplete-sequence ceiling

// isPartialOSCPrefix reports whether b might still grow into a sequence we parse.
func isPartialOSCPrefix(b []byte) bool {
	if len(b) == 0 {
		return false
	}
	if b[0] != 0x1b {
		return false
	}
	if len(b) == 1 {
		return true // lone ESC
	}
	switch b[1] {
	case ']':
		// OSC — incomplete until BEL or ST (parse returned false).
		return true
	case 'P':
		const tmuxPrefix = "\x1bPtmux;"
		if len(b) < len(tmuxPrefix) {
			return bytes.HasPrefix([]byte(tmuxPrefix), b)
		}
		return bytes.HasPrefix(b, []byte(tmuxPrefix))
	default:
		return false
	}
}

func indexOSCStart(b []byte) int {
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b {
			continue
		}
		if i+1 >= len(b) {
			return i // incomplete ESC
		}
		switch b[i+1] {
		case ']', 'P':
			return i
		}
	}
	return -1
}

// parseOSC52 tries to parse an OSC 52 (optionally tmux-DCS-wrapped) sequence at
// the start of b. ok=false means need more data or not OSC 52 (caller advances).
// end is the byte length of the full sequence to consume.
func parseOSC52(b []byte) (end int, payload []byte, ok bool) {
	if len(b) < 2 || b[0] != 0x1b {
		return 0, nil, false
	}

	// tmux DCS: ESC P tmux ; <payload with doubled ESC> ESC \
	if b[1] == 'P' {
		return parseTmuxDCSOSC52(b)
	}
	if b[1] != ']' {
		return 0, nil, false
	}
	return parsePlainOSC52(b, 0)
}

// parsePlainOSC52 parses ESC ] 52 ; target ; data ST starting at b[off] for the
// ESC (i.e. b[off]==ESC, b[off+1]==']'), but off is start of ESC.
func parsePlainOSC52(b []byte, start int) (end int, payload []byte, ok bool) {
	// Need at least ESC ] 52 ; x ; ST-min
	if start+5 > len(b) {
		return 0, nil, false
	}
	if b[start] != 0x1b || b[start+1] != ']' {
		return 0, nil, false
	}
	// Find terminator BEL (0x07) or ESC \
	term := -1
	termLen := 0
	for i := start + 2; i < len(b); i++ {
		if b[i] == 0x07 {
			term = i
			termLen = 1
			break
		}
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == '\\' {
			term = i
			termLen = 2
			break
		}
		// Guard runaway non-OSC junk: OSC params shouldn't contain bare ESC
		// except as ST start (handled above). If we see ESC ] again, abort.
		if b[i] == 0x1b && i+1 < len(b) && b[i+1] == ']' {
			return 0, nil, false
		}
	}
	if term < 0 {
		return 0, nil, false // incomplete
	}
	body := string(b[start+2 : term]) // after ESC ]
	end = term + termLen
	// body: "52;c;<b64>" or "52;;<b64>" or "52;c;?"
	// Non-clipboard OSC (e.g. ESC ] 0 ; title BEL) — consume, no copy.
	if !strings.HasPrefix(body, "52;") {
		return end, nil, true
	}
	rest := body[3:] // after "52;"
	// split target ; data
	semi := strings.IndexByte(rest, ';')
	if semi < 0 {
		return end, nil, true
	}
	data := rest[semi+1:]
	if data == "" || data == "?" {
		// query or clear — no payload to copy
		return end, nil, true
	}
	// base64 may be standard or raw; try both
	raw, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.RawStdEncoding.DecodeString(data)
	}
	if err != nil {
		// Invalid b64: still consume sequence so we don't stall.
		return end, nil, true
	}
	return end, raw, true
}

func parseTmuxDCSOSC52(b []byte) (end int, payload []byte, ok bool) {
	// ESC P tmux ; … ESC \
	if len(b) < 2 {
		return 0, nil, false
	}
	// Incomplete prefix of "\x1bPtmux;" — wait for more.
	const tmuxPrefix = "\x1bPtmux;"
	if len(b) < len(tmuxPrefix) {
		if bytes.HasPrefix([]byte(tmuxPrefix), b) {
			return 0, nil, false
		}
		// Cannot become tmux DCS — not ours; drain will skip a byte.
		return 0, nil, false
	}
	if !bytes.HasPrefix(b, []byte(tmuxPrefix)) {
		// Other DCS (not tmux clipboard wrap) — not ours.
		return 0, nil, false
	}
	// Find ESC \ terminator
	term := -1
	for i := 7; i+1 < len(b); i++ {
		if b[i] == 0x1b && b[i+1] == '\\' {
			term = i
			break
		}
	}
	if term < 0 {
		return 0, nil, false
	}
	inner := b[7:term]
	// tmux doubles ESC as ESC ESC inside DCS; unescape.
	unescaped := unescapeTmuxDCS(inner)
	// inner should contain ESC ] 52 ; … BEL (or ESC \)
	// Often: ESC ESC ] 52 ; c ; b64 BEL
	// After unescape: ESC ] 52 ; c ; b64 BEL
	end = term + 2
	// Find OSC 52 inside unescaped
	idx := bytes.Index(unescaped, []byte("\x1b]52;"))
	if idx < 0 {
		return end, nil, true // consume DCS, nothing to copy
	}
	_, payload, parsed := parsePlainOSC52(unescaped, idx)
	if !parsed {
		// incomplete inner shouldn't happen if DCS is complete; skip copy
		return end, nil, true
	}
	return end, payload, true
}

func unescapeTmuxDCS(in []byte) []byte {
	out := make([]byte, 0, len(in))
	for i := 0; i < len(in); i++ {
		if in[i] == 0x1b && i+1 < len(in) && in[i+1] == 0x1b {
			out = append(out, 0x1b)
			i++
			continue
		}
		out = append(out, in[i])
	}
	return out
}

func passthroughEnabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("GRAIN_OSC52_PASSTHROUGH")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	// Apple Terminal does not apply OSC 52; re-emitting is harmless but
	// default keep passthrough for iTerm/Ghostty/etc.
	return true
}

// Enabled reports whether Shell should wrap stdout with OSC 52 interception.
func Enabled() bool {
	v := strings.TrimSpace(strings.ToLower(os.Getenv("GRAIN_OSC52_CLIPBOARD")))
	if v == "0" || v == "false" || v == "no" || v == "off" {
		return false
	}
	// Alias
	v2 := strings.TrimSpace(strings.ToLower(os.Getenv("GRAIN_CLIPBOARD")))
	if v2 == "0" || v2 == "false" || v2 == "no" || v2 == "off" {
		return false
	}
	return true
}

func looksLikeImage(b []byte) bool {
	if len(b) < 4 {
		return false
	}
	// PNG
	if b[0] == 0x89 && b[1] == 'P' && b[2] == 'N' && b[3] == 'G' {
		return true
	}
	// JPEG
	if b[0] == 0xff && b[1] == 0xd8 {
		return true
	}
	// GIF
	if len(b) >= 6 && string(b[:3]) == "GIF" {
		return true
	}
	// WebP
	if len(b) >= 12 && string(b[:4]) == "RIFF" && string(b[8:12]) == "WEBP" {
		return true
	}
	return false
}

var errNoClipboard = errString("no host clipboard helper (pbcopy/wl-copy/xclip/xsel)")

type errString string

func (e errString) Error() string { return string(e) }

func runClipboardCmd(data []byte, name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Stdin = bytes.NewReader(data)
	// Discard helper noise
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	return cmd.Run()
}

func runClipboardOut(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil {
		return nil, err
	}
	return out, nil
}
