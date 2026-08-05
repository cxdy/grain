package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Guest clipboard helpers emit OSC 52 on /dev/tty so grain sh (local or remote)
// can intercept and write the client host clipboard. Sandboxes have no
// pbcopy/xclip/wl-copy; TUIs like Grok Build then report "clipboard unavailable"
// and never emit OSC 52 themselves.

const clipboardBinDir = "/var/lib/grain/bin"

var clipboardOnce sync.Once

// ensureClipboardHelpers writes OSC 52 shims (pbcopy, xclip, wl-copy, …) once
// per agent process and returns the bin directory to prepend to PATH.
// Safe to call from multiple shell sessions.
func ensureClipboardHelpers() (binDir string, err error) {
	var onceErr error
	clipboardOnce.Do(func() {
		onceErr = writeClipboardHelpers(clipboardBinDir)
	})
	if onceErr != nil {
		return "", onceErr
	}
	return clipboardBinDir, nil
}

func writeClipboardHelpers(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	// Shared body: read stdin → base64 → OSC 52 clipboard set on the TTY.
	// Using /dev/tty is required: helpers are often invoked with stdout redirected.
	const osc52Copy = `#!/bin/sh
# grain: copy stdin to client clipboard via OSC 52 (intercepted by grain sh)
if command -v base64 >/dev/null 2>&1; then
  b64=$(base64 | tr -d '\n\r') || exit 1
elif command -v openssl >/dev/null 2>&1; then
  b64=$(openssl base64 -A 2>/dev/null || openssl base64 | tr -d '\n\r') || exit 1
else
  echo "grain clipboard: base64 not found" >&2
  exit 1
fi
# ST = BEL — widely accepted; write to controlling TTY when present
seq=$(printf '\033]52;c;%s\007' "$b64")
# Redirect both fd 1 and 2 so a missing /dev/tty does not print shell errors.
if ! { printf '%s' "$seq" >/dev/tty; } 2>/dev/null; then
  # No controlling TTY (e.g. grain x / non-interactive): still emit on stdout
  printf '%s' "$seq"
fi
`

	// pbpaste fetches the client host clipboard via the agent (shell session
	// must be active — grain sh asks the laptop for paste data).
	const osc52Paste = `#!/bin/sh
# grain: paste from client clipboard via grain-agent GET /clipboard
# Supports text and image/png|jpeg (host grain sh reads screenshot pasteboard types).
url="http://127.0.0.1:7475/clipboard"
if command -v curl >/dev/null 2>&1; then
  exec curl -sf --max-time 20 "$url"
fi
if command -v wget >/dev/null 2>&1; then
  exec wget -q -O - --timeout=20 "$url"
fi
echo "grain clipboard: curl/wget required for paste" >&2
exit 1
`

	// xclip-compatible: honor -i/-o and ignore -selection args.
	const xclipShim = `#!/bin/sh
# grain: xclip shim → OSC 52 copy (or paste stub)
mode=copy
while [ $# -gt 0 ]; do
  case "$1" in
    -o|--output|-out) mode=paste; shift ;;
    -i|--input|-in) mode=copy; shift ;;
    -selection|-sel|-s)
      shift
      [ $# -gt 0 ] && shift
      ;;
    -loops|-l|-display|-d|-target|-t|-filter|-f)
      shift
      [ $# -gt 0 ] && shift
      ;;
    -*) shift ;;
    *) shift ;;
  esac
done
if [ "$mode" = paste ]; then
  exec "$(dirname "$0")/pbpaste"
fi
exec "$(dirname "$0")/pbcopy"
`

	const wlCopyShim = `#!/bin/sh
# grain: wl-copy shim → OSC 52
exec "$(dirname "$0")/pbcopy"
`

	const wlPasteShim = `#!/bin/sh
exec "$(dirname "$0")/pbpaste"
`

	const xselShim = `#!/bin/sh
# grain: xsel shim → OSC 52 copy / paste stub
mode=copy
while [ $# -gt 0 ]; do
  case "$1" in
    -o|--output) mode=paste; shift ;;
    -i|--input) mode=copy; shift ;;
    *) shift ;;
  esac
done
if [ "$mode" = paste ]; then
  exec "$(dirname "$0")/pbpaste"
fi
exec "$(dirname "$0")/pbcopy"
`

	files := map[string]string{
		"pbcopy":   osc52Copy,
		"pbpaste":  osc52Paste,
		"xclip":    xclipShim,
		"xsel":     xselShim,
		"wl-copy":  wlCopyShim,
		"wl-paste": wlPasteShim,
	}
	for name, body := range files {
		path := filepath.Join(dir, name)
		// Always refresh so upgrades pick up new shim text.
		if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
			return fmt.Errorf("clipboard helper %s: %w", name, err)
		}
	}
	return nil
}

// pathWithClipboardBin prepends clipboardBinDir to a PATH=… env entry value.
func pathWithClipboardBin(pathEnv string) string {
	const prefix = "PATH="
	val := pathEnv
	if len(pathEnv) >= len(prefix) && pathEnv[:len(prefix)] == prefix {
		val = pathEnv[len(prefix):]
	}
	if val == "" {
		return "PATH=" + clipboardBinDir
	}
	// Avoid duplicating.
	if val == clipboardBinDir || len(val) > len(clipboardBinDir) && val[:len(clipboardBinDir)+1] == clipboardBinDir+":" {
		return "PATH=" + val
	}
	return "PATH=" + clipboardBinDir + ":" + val
}
