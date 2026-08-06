package agent

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// LinuxBinaryName returns the expected filename for the guest agent binary
// built for Linux and the given GOARCH (defaults to runtime.GOARCH).
// Example: grain-agent-linux-arm64
func LinuxBinaryName(goarch string) string {
	if goarch == "" {
		goarch = runtime.GOARCH
	}
	return "grain-agent-linux-" + goarch
}

// LinuxBinaryPath locates a prebuilt grain-agent Linux binary for SSH deploy
// into guests. Search order (first existing regular file wins):
//
//  1. {dataDir}/agent/grain-agent-linux-$GOARCH — install/update cache (preferred)
//  2. Same directory as the running executable
//  3. bin/ next to the executable
//  4. ./bin/ and ./ relative to the process working directory (dev checkouts last)
//
// dataDir is preferred over cwd so `grain agent deploy` from a git checkout does
// not ship a stale repo bin/ over the agent installed by `grain update`.
// Host GOARCH is used as the guest arch (local QEMU typically matches the host).
// Run `just agent-linux` to produce these binaries before live deploy.
func LinuxBinaryPath(dataDir string) (string, error) {
	name := LinuxBinaryName(runtime.GOARCH)
	var candidates []string

	if dataDir != "" {
		candidates = append(candidates, filepath.Join(dataDir, "agent", name))
	}
	if exe, err := os.Executable(); err == nil {
		if resolved, err := filepath.EvalSymlinks(exe); err == nil {
			exe = resolved
		}
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, name),
			filepath.Join(dir, "bin", name),
		)
	}
	if cwd, err := os.Getwd(); err == nil {
		candidates = append(candidates,
			filepath.Join(cwd, "bin", name),
			filepath.Join(cwd, name),
		)
	}

	seen := make(map[string]struct{}, len(candidates))
	var searched []string
	for _, p := range candidates {
		if p == "" {
			continue
		}
		abs := p
		if a, err := filepath.Abs(p); err == nil {
			abs = a
		}
		if _, ok := seen[abs]; ok {
			continue
		}
		seen[abs] = struct{}{}
		searched = append(searched, abs)
		st, err := os.Stat(abs)
		if err != nil || st.IsDir() {
			continue
		}
		return abs, nil
	}
	return "", fmt.Errorf("linux agent binary %s not found (run just agent-linux); searched: %s",
		name, strings.Join(searched, ", "))
}
