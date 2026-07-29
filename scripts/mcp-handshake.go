// Command mcp-handshake drives initialize + tools/list against `grain mcp` over stdio.
//
//	go run ./scripts/mcp-handshake.go ./bin/grain
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
	bin := "./bin/grain"
	if len(os.Args) > 1 {
		bin = os.Args[1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "mcp")
	cmd.Env = os.Environ()

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

	need := []string{
		"grain_health", "grain_list_vms", "grain_get_vm", "grain_create_vm",
		"grain_start_vm", "grain_stop_vm", "grain_delete_vm", "grain_exec",
		"grain_write_file", "grain_read_file", "grain_agent_health", "grain_logs",
		"grain_stats", "grain_workspace_sandbox", "grain_forward_add", "grain_forward_remove",
		"grain_image_list", "grain_image_pull", "grain_fs_readdir", "grain_act", "grain_k3s",
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
	fmt.Println("expanded tools: ok")
}
