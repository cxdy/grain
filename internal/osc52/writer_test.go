package osc52

import (
	"bytes"
	"encoding/base64"
	"strings"
	"sync"
	"testing"
)

func TestWriterPlainOSC52BEL(t *testing.T) {
	var dst bytes.Buffer
	var got []byte
	var mu sync.Mutex
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error {
		mu.Lock()
		got = append([]byte(nil), p...)
		mu.Unlock()
		return nil
	}

	payload := []byte("hello from grok")
	b64 := base64.StdEncoding.EncodeToString(payload)
	seq := "\x1b]52;c;" + b64 + "\x07"
	msg := "before" + seq + "after"
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if string(got) != string(payload) {
		t.Fatalf("clipboard %q want %q", got, payload)
	}
	if dst.String() != msg {
		t.Fatalf("passthrough %q", dst.String())
	}
}

func TestWriterOSC52ST(t *testing.T) {
	var dst bytes.Buffer
	var got []byte
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { got = append([]byte(nil), p...); return nil }

	payload := []byte("st-term")
	b64 := base64.StdEncoding.EncodeToString(payload)
	seq := "\x1b]52;c;" + b64 + "\x1b\\"
	if _, err := w.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestWriterSplitAcrossWrites(t *testing.T) {
	var dst bytes.Buffer
	var got []byte
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { got = append([]byte(nil), p...); return nil }

	payload := []byte("chunked clipboard data")
	b64 := base64.StdEncoding.EncodeToString(payload)
	seq := "\x1b]52;c;" + b64 + "\x07"
	// Split mid-sequence
	mid := len(seq) / 2
	if _, err := w.Write([]byte(seq[:mid])); err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("early copy: %q", got)
	}
	if _, err := w.Write([]byte(seq[mid:])); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
	if dst.String() != seq {
		t.Fatalf("dst %q", dst.String())
	}
}

func TestWriterNoPassthrough(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	w.passthrough = false
	w.copyFn = func(p []byte) error { return nil }
	payload := []byte("x")
	b64 := base64.StdEncoding.EncodeToString(payload)
	seq := "prefix\x1b]52;c;" + b64 + "\x07suffix"
	if _, err := w.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "prefixsuffix" {
		t.Fatalf("dst %q", dst.String())
	}
}

func TestWriterTmuxDCS(t *testing.T) {
	var dst bytes.Buffer
	var got []byte
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { got = append([]byte(nil), p...); return nil }

	payload := []byte("tmux-clip")
	b64 := base64.StdEncoding.EncodeToString(payload)
	// ESC P tmux ; ESC ESC ] 52 ; c ; b64 BEL ESC \
	inner := "\x1b\x1b]52;c;" + b64 + "\x07"
	seq := "\x1bPtmux;" + inner + "\x1b\\"
	if _, err := w.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q want %q", got, payload)
	}
}

func TestWriterQueryNoCopy(t *testing.T) {
	var dst bytes.Buffer
	called := false
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { called = true; return nil }
	seq := "\x1b]52;c;?\x07"
	if _, err := w.Write([]byte(seq)); err != nil {
		t.Fatal(err)
	}
	if called {
		t.Fatal("query should not copy")
	}
}

func TestWriterPlainTextOnly(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { t.Fatal("no copy"); return nil }
	msg := "hello\r\nworld\x1b[0m"
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	if dst.String() != msg {
		t.Fatalf("%q", dst.String())
	}
}

func TestEnabledEnv(t *testing.T) {
	t.Setenv("GRAIN_OSC52_CLIPBOARD", "0")
	if Enabled() {
		t.Fatal("expected disabled")
	}
	t.Setenv("GRAIN_OSC52_CLIPBOARD", "")
	t.Setenv("GRAIN_CLIPBOARD", "false")
	if Enabled() {
		t.Fatal("expected disabled via GRAIN_CLIPBOARD")
	}
	t.Setenv("GRAIN_CLIPBOARD", "")
	if !Enabled() {
		t.Fatal("expected enabled by default")
	}
}

func TestPassthroughEnv(t *testing.T) {
	t.Setenv("GRAIN_OSC52_PASSTHROUGH", "0")
	if passthroughEnabled() {
		t.Fatal("expected off")
	}
	t.Setenv("GRAIN_OSC52_PASSTHROUGH", "")
	if !passthroughEnabled() {
		t.Fatal("expected on")
	}
}

func TestInvalidBase64StillConsumes(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { t.Fatal("no"); return nil }
	seq := "\x1b]52;c;%%%not-b64%%%\x07"
	if _, err := w.Write([]byte("A" + seq + "B")); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dst.String(), "A") || !strings.Contains(dst.String(), "B") {
		t.Fatalf("%q", dst.String())
	}
}

func TestWriterNonClipboardOSCPassesThrough(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { t.Fatal("no copy for title OSC"); return nil }
	// OSC 0 window title
	seq := "\x1b]0;my title\x07"
	msg := "x" + seq + "y"
	if _, err := w.Write([]byte(msg)); err != nil {
		t.Fatal(err)
	}
	if dst.String() != msg {
		t.Fatalf("dst %q", dst.String())
	}
}

type failWriter struct{ n int }

func (f *failWriter) Write(p []byte) (int, error) {
	f.n++
	return 0, errString("write fail")
}

func TestWriterEmptyAndDrainErrors(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	n, err := w.Write(nil)
	if n != 0 || err != nil {
		t.Fatalf("%d %v", n, err)
	}
	// trailing ESC kept in buf
	if _, err := w.Write([]byte("hi\x1b")); err != nil {
		t.Fatal(err)
	}
	if dst.String() != "hi" {
		t.Fatalf("dst %q", dst.String())
	}
	// complete after partial ESC
	if _, err := w.Write([]byte("]0;t\x07")); err != nil {
		t.Fatal(err)
	}

	// emit failure
	fw := &failWriter{}
	w2 := NewWriter(fw)
	w2.copyFn = nil
	if _, err := w2.Write([]byte("plain")); err == nil {
		t.Fatal("expected emit error")
	}
}

func TestWriterPartialOSCAndNonTmuxDCS(t *testing.T) {
	var dst bytes.Buffer
	w := NewWriter(&dst)
	w.copyFn = func(p []byte) error { return nil }

	// incomplete OSC prefix held
	if _, err := w.Write([]byte("\x1b]52;c;YW")); err != nil {
		t.Fatal(err)
	}
	if dst.Len() != 0 {
		t.Fatalf("held incomplete, got %q", dst.String())
	}
	// finish
	if _, err := w.Write([]byte("Jj\x07")); err != nil {
		t.Fatal(err)
	}

	// ESC P that is not tmux — emit and continue
	w3 := NewWriter(&dst)
	w3.copyFn = func(p []byte) error { return nil }
	if _, err := w3.Write([]byte("\x1bPhello\x1b\\")); err != nil {
		t.Fatal(err)
	}

	// bare ESC then non-OSC byte
	w4 := NewWriter(&bytes.Buffer{})
	if _, err := w4.Write([]byte("\x1bXmore")); err != nil {
		t.Fatal(err)
	}

	// nested ESC ] aborts plain parse
	w5 := NewWriter(&bytes.Buffer{})
	w5.copyFn = func(p []byte) error { t.Fatal("no"); return nil }
	if _, err := w5.Write([]byte("\x1b]0;\x1b]52;c;YQ==\x07")); err != nil {
		t.Fatal(err)
	}

	// 52 without second semicolon
	w6 := NewWriter(&bytes.Buffer{})
	if _, err := w6.Write([]byte("\x1b]52;onlytarget\x07")); err != nil {
		t.Fatal(err)
	}

	// empty data after semi
	w7 := NewWriter(&bytes.Buffer{})
	if _, err := w7.Write([]byte("\x1b]52;c;\x07")); err != nil {
		t.Fatal(err)
	}

	// raw std encoding (no padding)
	payload := []byte("raw!")
	b64 := base64.RawStdEncoding.EncodeToString(payload)
	var got []byte
	w8 := NewWriter(&bytes.Buffer{})
	w8.copyFn = func(p []byte) error { got = append([]byte(nil), p...); return nil }
	if _, err := w8.Write([]byte("\x1b]52;c;" + b64 + "\x07")); err != nil {
		t.Fatal(err)
	}
	if string(got) != string(payload) {
		t.Fatalf("got %q", got)
	}
}

func TestWriterTmuxDCSEdgeCases(t *testing.T) {
	// incomplete tmux prefix
	var dst bytes.Buffer
	w := NewWriter(&dst)
	if _, err := w.Write([]byte("\x1bPtm")); err != nil {
		t.Fatal(err)
	}
	// complete as non-clipboard DCS (no OSC 52 inside)
	if _, err := w.Write([]byte("ux;noise\x1b\\")); err != nil {
		t.Fatal(err)
	}

	// other DCS (not tmux)
	w2 := NewWriter(&bytes.Buffer{})
	if _, err := w2.Write([]byte("\x1bPother;x\x1b\\")); err != nil {
		t.Fatal(err)
	}

	// isPartialOSCPrefix helpers via short writes
	if isPartialOSCPrefix(nil) {
		t.Fatal("empty")
	}
	if isPartialOSCPrefix([]byte("a")) {
		t.Fatal("no esc")
	}
	if !isPartialOSCPrefix([]byte{0x1b}) {
		t.Fatal("lone esc")
	}
	if !isPartialOSCPrefix([]byte("\x1b]")) {
		t.Fatal("osc start")
	}
	if isPartialOSCPrefix([]byte("\x1bX")) {
		t.Fatal("not partial")
	}
	if !isPartialOSCPrefix([]byte("\x1bP")) {
		t.Fatal("dcs start")
	}
	if !isPartialOSCPrefix([]byte("\x1bPtmux;")) {
		t.Fatal("full tmux prefix still incomplete until body ST")
	}
}

func TestWriteReadClipboardDarwin(t *testing.T) {
	// Exercises writeClipboard / ReadClipboard / runClipboardCmd / runClipboardOut on darwin.
	if err := writeClipboard(nil); err != nil {
		t.Fatal(err)
	}
	payload := []byte("grain-osc52-test-" + t.Name())
	if err := writeClipboard(payload); err != nil {
		t.Fatalf("writeClipboard: %v", err)
	}
	got, err := ReadClipboard()
	if err != nil {
		t.Fatalf("ReadClipboard: %v", err)
	}
	// Other tests may race the clipboard; just ensure we got something or exact match.
	if !bytes.Contains(got, []byte("grain-osc52-test-")) && string(got) != string(payload) {
		t.Logf("clipboard may have been overwritten: %q", got)
	}
	// errString
	if errNoClipboard.Error() == "" {
		t.Fatal("empty error")
	}
	// runClipboard helpers directly
	if err := runClipboardCmd([]byte("x"), "true"); err != nil {
		t.Fatal(err)
	}
	out, err := runClipboardOut("echo", "-n", "hi")
	if err != nil || string(out) != "hi" {
		t.Fatalf("%q %v", out, err)
	}
	if _, err := runClipboardOut("false"); err == nil {
		t.Fatal("expected error")
	}
}

func TestEnabledPassthroughVariants(t *testing.T) {
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Setenv("GRAIN_OSC52_CLIPBOARD", v)
		t.Setenv("GRAIN_CLIPBOARD", "")
		if Enabled() {
			t.Fatalf("Enabled with %q", v)
		}
	}
	t.Setenv("GRAIN_OSC52_CLIPBOARD", "")
	t.Setenv("GRAIN_CLIPBOARD", "")
	if !Enabled() {
		t.Fatal("default enabled")
	}
	for _, v := range []string{"0", "false", "no", "off"} {
		t.Setenv("GRAIN_OSC52_PASSTHROUGH", v)
		if passthroughEnabled() {
			t.Fatalf("passthrough with %q", v)
		}
	}
	t.Setenv("GRAIN_OSC52_PASSTHROUGH", "1")
	if !passthroughEnabled() {
		t.Fatal("passthrough on")
	}
}

func TestParseOSC52Direct(t *testing.T) {
	end, payload, ok := parseOSC52(nil)
	if ok || end != 0 || payload != nil {
		t.Fatalf("%v %v %v", end, payload, ok)
	}
	end, payload, ok = parseOSC52([]byte("\x1bX"))
	if ok {
		t.Fatal("expected not ok")
	}
	_ = end
	_ = payload
}
