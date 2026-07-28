// Command mcp-handshake drives initialize + tools/list against a grain-mcp
// child process over stdio. Used for launch verification (not a reimplementation
// of tool registration — tools come from the child binary).
//
//	go run ./scripts/mcp-handshake.go ./bin/grain-mcp
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	bin := "./bin/grain-mcp"
	if len(os.Args) > 1 {
		bin = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin)
	cmd.Env = os.Environ()
	// Point at a mock HTTP daemon if GRAIN_API already set; else handshake
	// still works — tools/list does not require a live daemon call.
	// Dial happens at process start; ensure GRAIN_API is set by the caller when needed.

	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	// Prefer CommandTransport so we exercise the real binary's stdio path.
	_ = clientTransport
	_ = serverTransport

	// Use CommandTransport bound to the binary.
	ct := &mcp.CommandTransport{Command: cmd}
	cli := mcp.NewClient(&mcp.Implementation{Name: "handshake", Version: "0"}, nil)
	sess, err := cli.Connect(ctx, ct, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect: %v\n", err)
		os.Exit(1)
	}
	defer sess.Close()

	fmt.Println("initialize: ok")

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tools/list: %v\n", err)
		os.Exit(1)
	}
	names := make([]string, 0, len(tools.Tools))
	for _, t := range tools.Tools {
		names = append(names, t.Name)
		fmt.Printf("tool: %s — %s\n", t.Name, t.Description)
	}
	out, _ := json.MarshalIndent(map[string]any{
		"ok":    true,
		"count": len(names),
		"tools": names,
	}, "", "  ")
	fmt.Println(string(out))

	// Required lifecycle tools
	need := []string{
		"grain_health", "grain_list_vms", "grain_get_vm", "grain_create_vm",
		"grain_start_vm", "grain_stop_vm", "grain_delete_vm", "grain_exec",
	}
	have := map[string]bool{}
	for _, n := range names {
		have[n] = true
	}
	for _, n := range need {
		if !have[n] {
			fmt.Fprintf(os.Stderr, "missing required tool %q\n", n)
			os.Exit(1)
		}
	}
	fmt.Println("lifecycle tools: ok")
}
