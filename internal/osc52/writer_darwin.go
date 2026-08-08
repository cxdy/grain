//go:build darwin

package osc52

import (
	"io"
	"os"
	"os/exec"
	"sync"
)

// writeClipboard copies data to the host system clipboard.
func writeClipboard(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return runClipboardCmd(data, "pbcopy")
}

// ReadClipboard returns the host system clipboard as bytes (for paste into guest).
func ReadClipboard() ([]byte, error) {
	return readClipboardDarwin()
}

// readClipboardDarwin prefers PNG (converting TIFF/JPEG when needed), then text.
func readClipboardDarwin() ([]byte, error) {
	if img, err := readDarwinClipboardImage(); err == nil && len(img) > 0 {
		return img, nil
	}
	text, err := runClipboardOut("pbpaste")
	if err != nil {
		return nil, err
	}
	if len(text) == 0 {
		return nil, errString("clipboard empty or image type unsupported (try copying again)")
	}
	return text, nil
}

const darwinClipboardSwiftSrc = `
import AppKit
import Foundation

func writePNG(_ data: Data) {
  FileHandle.standardOutput.write(data)
  exit(0)
}

func tiffToPNG(_ tiff: Data) -> Data? {
  guard let img = NSImage(data: tiff) else { return nil }
  var rect = NSRect(origin: .zero, size: img.size)
  // Full-size CGImage; avoids icon-sized NSBitmapImageRep(data:) picks.
  guard let cg = img.cgImage(forProposedRect: &rect, context: nil, hints: nil) else {
    // Fallback: largest bitmap rep on the image.
    var best: NSBitmapImageRep?
    for rep in img.representations {
      guard let b = rep as? NSBitmapImageRep else { continue }
      if best == nil || (b.pixelsWide * b.pixelsHigh) > (best!.pixelsWide * best!.pixelsHigh) {
        best = b
      }
    }
    return best?.representation(using: .png, properties: [:])
  }
  let rep = NSBitmapImageRep(cgImage: cg)
  return rep.representation(using: .png, properties: [:])
}

let pb = NSPasteboard.general
if let png = pb.data(forType: .png), !png.isEmpty {
  writePNG(png)
}
if let jpeg = pb.data(forType: NSPasteboard.PasteboardType("public.jpeg")), !jpeg.isEmpty {
  FileHandle.standardOutput.write(jpeg)
  exit(0)
}
// .tiff and public.tiff (screenshot / Continuity Camera)
let tiffType = NSPasteboard.PasteboardType.tiff
let publicTIFF = NSPasteboard.PasteboardType("public.tiff")
if let tiff = pb.data(forType: tiffType) ?? pb.data(forType: publicTIFF), !tiff.isEmpty,
   let png = tiffToPNG(tiff), !png.isEmpty {
  writePNG(png)
}
// Some apps only expose NSImage via readObjects
if let imgs = pb.readObjects(forClasses: [NSImage.self], options: nil) as? [NSImage],
   let img = imgs.first {
  var rect = NSRect(origin: .zero, size: img.size)
  if let cg = img.cgImage(forProposedRect: &rect, context: nil, hints: nil) {
    let rep = NSBitmapImageRep(cgImage: cg)
    if let png = rep.representation(using: .png, properties: [:]), !png.isEmpty {
      writePNG(png)
    }
  }
}
exit(1)
`

var (
	darwinClipHelperOnce sync.Once
	darwinClipHelperPath string
	darwinClipHelperErr  error
)

// readDarwinClipboardImage returns PNG (or JPEG) bytes from the macOS pasteboard.
// Screenshots often land as TIFF only (no PNG type). Naive
// NSBitmapImageRep(data: tiff) can yield a tiny/icon rep; we rasterize via
// NSImage.cgImage at full size then encode PNG.
func readDarwinClipboardImage() ([]byte, error) {
	helper, err := darwinClipboardHelper()
	if err != nil {
		// Fallback: slow swift -e (first-run / no write cache).
		if _, lerr := exec.LookPath("swift"); lerr != nil {
			return nil, errString("swift not found for image clipboard")
		}
		cmd := exec.Command("swift", "-e", darwinClipboardSwiftSrc)
		cmd.Stderr = io.Discard
		out, oerr := cmd.Output()
		if oerr != nil || len(out) == 0 {
			return nil, errString("no image on clipboard")
		}
		return out, nil
	}
	cmd := exec.Command(helper)
	cmd.Stderr = io.Discard
	out, err := cmd.Output()
	if err != nil || len(out) == 0 {
		return nil, errString("no image on clipboard")
	}
	return out, nil
}

// darwinClipboardHelper compiles the AppKit paste reader once into ~/.grain/bin.
func darwinClipboardHelper() (string, error) {
	darwinClipHelperOnce.Do(func() {
		if _, err := exec.LookPath("swiftc"); err != nil {
			if _, err2 := exec.LookPath("swift"); err2 != nil {
				darwinClipHelperErr = errString("swift not found for image clipboard")
				return
			}
			// swiftc missing: callers fall back to swift -e.
			darwinClipHelperErr = errString("swiftc not found")
			return
		}
		home, err := os.UserHomeDir()
		if err != nil {
			darwinClipHelperErr = err
			return
		}
		dir := home + "/.grain/bin"
		if err := os.MkdirAll(dir, 0o755); err != nil {
			darwinClipHelperErr = err
			return
		}
		bin := dir + "/grain-clipboard-read"
		src := dir + "/grain-clipboard-read.swift"
		// Rebuild when source marker missing or binary absent.
		if st, err := os.Stat(bin); err == nil && st.Size() > 0 {
			darwinClipHelperPath = bin
			return
		}
		if err := os.WriteFile(src, []byte(darwinClipboardSwiftSrc), 0o644); err != nil {
			darwinClipHelperErr = err
			return
		}
		cmd := exec.Command("swiftc", "-O", "-o", bin, src)
		cmd.Stderr = io.Discard
		if err := cmd.Run(); err != nil {
			darwinClipHelperErr = err
			return
		}
		darwinClipHelperPath = bin
	})
	if darwinClipHelperErr != nil {
		return "", darwinClipHelperErr
	}
	if darwinClipHelperPath == "" {
		return "", errString("clipboard helper unavailable")
	}
	return darwinClipHelperPath, nil
}
