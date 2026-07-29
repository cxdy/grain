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
