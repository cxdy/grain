//go:build windows

package osc52

import (
	"io"
	"os/exec"
)

func writeClipboard(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return runClipboardCmd(data, "clip")
}

func ReadClipboard() ([]byte, error) {
	if img, err := readClipboardWindowsImage(); err == nil && len(img) > 0 {
		return img, nil
	}
	return runClipboardOut("powershell", "-NoProfile", "-Command", "Get-Clipboard -Raw")
}

func readClipboardWindowsImage() ([]byte, error) {
	const ps = `
Add-Type -AssemblyName System.Windows.Forms
$img = [System.Windows.Forms.Clipboard]::GetImage()
if ($null -eq $img) { exit 2 }
$ms = New-Object System.IO.MemoryStream
$img.Save($ms, [System.Drawing.Imaging.ImageFormat]::Png)
[Console]::OpenStandardOutput().Write($ms.ToArray(), 0, $ms.Length)
`
	cmd := exec.Command("powershell", "-NoProfile", "-Command", ps)
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil, errString("no image on clipboard")
	}
	return out, nil
}
