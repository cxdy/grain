//go:build linux

package osc52

import (
	"os/exec"
)

// writeClipboard copies data to the host system clipboard.
func writeClipboard(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	if _, err := exec.LookPath("wl-copy"); err == nil {
		return runClipboardCmd(data, "wl-copy")
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		return runClipboardCmd(data, "xclip", "-selection", "clipboard")
	}
	if _, err := exec.LookPath("xsel"); err == nil {
		return runClipboardCmd(data, "xsel", "--clipboard", "--input")
	}
	return errNoClipboard
}

// ReadClipboard returns the host system clipboard as bytes (for paste into guest).
func ReadClipboard() ([]byte, error) {
	return readClipboardLinux()
}

func readClipboardLinux() ([]byte, error) {
	if _, err := exec.LookPath("wl-paste"); err == nil {
		if img, err := runClipboardOut("wl-paste", "-t", "image/png"); err == nil && looksLikeImage(img) {
			return img, nil
		}
		if img, err := runClipboardOut("wl-paste", "-t", "image/jpeg"); err == nil && looksLikeImage(img) {
			return img, nil
		}
		if text, err := runClipboardOut("wl-paste"); err == nil && len(text) > 0 {
			return text, nil
		}
	}
	if _, err := exec.LookPath("xclip"); err == nil {
		if img, err := runClipboardOut("xclip", "-selection", "clipboard", "-t", "image/png", "-o"); err == nil && looksLikeImage(img) {
			return img, nil
		}
		if img, err := runClipboardOut("xclip", "-selection", "clipboard", "-t", "image/jpeg", "-o"); err == nil && looksLikeImage(img) {
			return img, nil
		}
		if text, err := runClipboardOut("xclip", "-selection", "clipboard", "-o"); err == nil && len(text) > 0 {
			return text, nil
		}
	}
	if _, err := exec.LookPath("xsel"); err == nil {
		if text, err := runClipboardOut("xsel", "--clipboard", "--output"); err == nil && len(text) > 0 {
			return text, nil
		}
	}
	return nil, errNoClipboard
}
