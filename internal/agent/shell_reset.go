package agent

import (
	"fmt"
	"io"
	"strings"
)

// resetTerminalAfterShell clears private modes that guest TUIs (Grok, tmux, …)
// enable on the client terminal. term.Restore only undoes raw mode; it does not
// leave the alternate screen, mouse tracking, or incomplete graphics modes.
//
// Written on every Shell() exit so a hard disconnect (daemon restart) does not
// leave the remote terminal unusable.
func resetTerminalAfterShell(w io.Writer) {
	if w == nil {
		return
	}
	// Soft reset + explicit mode offs. Avoid full RIS (\x1bc) which can clear
	// scrollback on some terminals.
	const seq = "" +
		"\x1b[?1000l" + // mouse X10 off
		"\x1b[?1002l" + // cell motion off
		"\x1b[?1003l" + // all motion off
		"\x1b[?1005l" + // UTF-8 mouse off
		"\x1b[?1006l" + // SGR mouse off
		"\x1b[?1015l" + // urxvt mouse off
		"\x1b[?2004l" + // bracketed paste off
		"\x1b[?2026l" + // synchronized output off
		"\x1b[?1049l" + // leave alt screen (1049)
		"\x1b[?47l" + // leave alt screen (legacy)
		"\x1b[?25h" + // show cursor
		"\x1b[0m" + // SGR reset
		"\x1b[?1l" + // normal cursor keys
		"\x1b[?7h" + // wraparound on
		"\x1b[<u" + // Kitty keyboard: pop mode stack
		"\x1b[?2004l" + // bracketed paste (again after soft paths)
		"\r\n"
	_, _ = io.WriteString(w, seq)
}

// isShellConnectionLost reports abrupt WebSocket/PTY loss (daemon restart, proxy
// drop, network blip) as opposed to a clean guest shell exit.
func isShellConnectionLost(err error) bool {
	if err == nil {
		return false
	}
	if isWSNormalClose(err) {
		return false
	}
	msg := strings.ToLower(err.Error())
	// coder/websocket and net errors when the daemon/API disappears mid-session.
	for _, sub := range []string{
		"failed to read frame header: eof",
		"failed to get reader: failed to read frame header",
		"failed to get reader: eof",
		"use of closed network connection",
		"connection reset by peer",
		"broken pipe",
		"unexpected eof",
		"connection refused",
		"i/o timeout",
	} {
		if strings.Contains(msg, sub) {
			return true
		}
	}
	return false
}

// wrapShellSessionError maps disconnect errors to a clearer message for remote users.
func wrapShellSessionError(err error) error {
	if err == nil || isWSNormalClose(err) {
		return nil
	}
	if isShellConnectionLost(err) {
		return fmt.Errorf("connection lost (host grain daemon restarted or network dropped); if the terminal is garbled, run: reset\n(%v)", err)
	}
	return err
}
