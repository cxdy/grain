package desktop

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// LogSource selects serial console vs QEMU process log.
type LogSource string

const (
	// LogSerial is ~/.grain/vms/<name>/serial.log
	LogSerial LogSource = "serial"
	// LogQEMU is ~/.grain/logs/<name>.log
	LogQEMU LogSource = "qemu"
)

// LogPath returns the host filesystem path for a VM log.
// dataDir should be the local grain data directory.
func LogPath(dataDir, vmName string, source LogSource) (string, error) {
	if strings.TrimSpace(vmName) == "" {
		return "", fmt.Errorf("vm name is required")
	}
	if strings.TrimSpace(dataDir) == "" {
		return "", fmt.Errorf("data_dir is required for logs")
	}
	dataDir = expandHome(dataDir)
	name := filepath.Base(vmName) // prevent path escape
	if name == "." || name == ".." || name == string(filepath.Separator) {
		return "", fmt.Errorf("invalid vm name")
	}
	switch source {
	case LogQEMU:
		return filepath.Join(dataDir, "logs", name+".log"), nil
	case LogSerial, "":
		return filepath.Join(dataDir, "vms", name, "serial.log"), nil
	default:
		return "", fmt.Errorf("unknown log source %q", source)
	}
}

// ReadLogsResult is the content of a log file for the UI.
type ReadLogsResult struct {
	Path    string `json:"path"`
	Content string `json:"content"`
	// Truncated is true when only the tail was returned.
	Truncated bool `json:"truncated"`
	// Missing is true when the file does not exist.
	Missing bool `json:"missing"`
}

// ReadLogs reads up to maxBytes from the end of the log file (tail).
// maxBytes <= 0 means 256KiB default. Local data_dir only (CLI parity).
func ReadLogs(dataDir, vmName string, source LogSource, maxBytes int64) (ReadLogsResult, error) {
	path, err := LogPath(dataDir, vmName, source)
	if err != nil {
		return ReadLogsResult{}, err
	}
	if maxBytes <= 0 {
		maxBytes = 256 << 10
	}
	res := ReadLogsResult{Path: path}
	fi, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			res.Missing = true
			return res, nil
		}
		return res, err
	}
	f, err := os.Open(path)
	if err != nil {
		return res, err
	}
	defer func() { _ = f.Close() }()

	size := fi.Size()
	var start int64
	if size > maxBytes {
		start = size - maxBytes
		res.Truncated = true
	}
	if start > 0 {
		if _, err := f.Seek(start, io.SeekStart); err != nil {
			return res, err
		}
	}
	b, err := io.ReadAll(io.LimitReader(f, maxBytes+1))
	if err != nil {
		return res, err
	}
	// If we sought mid-file, drop partial first line for cleaner UI.
	content := string(b)
	if res.Truncated {
		if i := strings.IndexByte(content, '\n'); i >= 0 && i+1 < len(content) {
			content = content[i+1:]
		}
	}
	res.Content = content
	return res, nil
}

// LogsDataDir picks data_dir for logs: connection override, else config.
func LogsDataDir(conn Connection, cfgDataDir string) string {
	if d := strings.TrimSpace(conn.DataDir); d != "" {
		return expandHome(d)
	}
	return expandHome(cfgDataDir)
}

// CanReadLocalLogs is true when the connection is local (host can see serial.log).
func CanReadLocalLogs(conn Connection) bool {
	return conn.IsLocal()
}
