package main

import (
	"errors"
	"fmt"
	"os"
	"os/exec"

	"github.com/cxdy/grain/internal/cli"
)

// Default for plain `go build`. Release and `just build` inject the real
// version via -ldflags "-X main.version=...".
var version = "dev"

// exitCoder is implemented by *exec.ExitError and cli exitCodeError.
type exitCoder interface {
	ExitCode() int
}

func main() {
	if err := cli.Root(version).Execute(); err != nil {
		var code int
		var ec exitCoder
		var ee *exec.ExitError
		switch {
		case errors.As(err, &ee):
			code = ee.ExitCode()
			// Match classic ssh: no "error:" prefix for remote exit status.
			os.Exit(code)
		case errors.As(err, &ec):
			code = ec.ExitCode()
			// Bare exit-status errors from agent path — exit silently (stdout/stderr already printed).
			if err.Error() == fmt.Sprintf("exit status %d", code) {
				os.Exit(code)
			}
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(code)
		default:
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}
}
