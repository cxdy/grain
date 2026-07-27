//go:build !cgo || windows

package tray

import "fmt"

// Run is unavailable in CGO-disabled release builds and on Windows.
func Run(opts Options) error {
	_ = opts
	return fmt.Errorf("grain tray requires a CGO-enabled build on macOS or Linux (release archives are CGO_ENABLED=0; build locally with CGO_ENABLED=1)")
}
