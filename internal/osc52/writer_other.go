//go:build !darwin && !linux && !windows

package osc52

func writeClipboard(data []byte) error {
	if len(data) == 0 {
		return nil
	}
	return errNoClipboard
}

func ReadClipboard() ([]byte, error) {
	return nil, errNoClipboard
}
