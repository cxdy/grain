package agent

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestResetTerminalAfterShell(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	resetTerminalAfterShell(&buf)
	s := buf.String()
	for _, want := range []string{
		"\x1b[?1000l", // mouse off
		"\x1b[?1006l", // SGR mouse off
		"\x1b[?2004l", // bracketed paste off
		"\x1b[?1049l", // leave alt screen
		"\x1b[?25h",   // show cursor
		"\x1b[0m",     // SGR
		"\x1b[<u",     // kitty keyboard pop
	} {
		if !strings.Contains(s, want) {
			t.Fatalf("missing %q in reset seq %q", want, s)
		}
	}
	resetTerminalAfterShell(nil) // no panic
	resetTerminalAfterShell(io.Discard)
}

func TestIsShellConnectionLost(t *testing.T) {
	t.Parallel()
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{errors.New("failed to get reader: failed to read frame header: EOF"), true},
		{errors.New("failed to read frame header: EOF"), true},
		{errors.New("read tcp 127.0.0.1:1: connection reset by peer"), true},
		{errors.New("write: broken pipe"), true},
		{errors.New("use of closed network connection"), true},
		{errors.New("something else"), false},
	}
	for _, tc := range cases {
		if got := isShellConnectionLost(tc.err); got != tc.want {
			t.Errorf("%v: got %v want %v", tc.err, got, tc.want)
		}
	}
}

func TestWrapShellSessionError(t *testing.T) {
	t.Parallel()
	if wrapShellSessionError(nil) != nil {
		t.Fatal("nil")
	}
	err := wrapShellSessionError(errors.New("failed to get reader: failed to read frame header: EOF"))
	if err == nil || !strings.Contains(err.Error(), "connection lost") || !strings.Contains(err.Error(), "reset") {
		t.Fatalf("%v", err)
	}
	other := errors.New("permission denied")
	if wrapShellSessionError(other).Error() != other.Error() {
		t.Fatalf("passthrough: %v", wrapShellSessionError(other))
	}
}
