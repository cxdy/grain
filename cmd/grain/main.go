package main

import (
	"fmt"
	"os"

	"github.com/cxdy/grain/internal/cli"
)

// Set via -ldflags "-X main.version=..."
var version = "0.1.0-dev"

func main() {
	if err := cli.Root(version).Execute(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
