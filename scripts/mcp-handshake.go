// Command mcp-handshake drives initialize + tools/list against `grain mcp` over stdio.
//
//	go run ./scripts/mcp-handshake.go ./bin/grain
package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	if err := runHandshake(os.Args); err != nil {
		fmt.Fprintf(os.Stderr, "%v\n", err)
		os.Exit(1)
	}
}

func runHandshake(args []string) error {
	bin := pickGrainBin(args)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bin, "mcp")
	cmd.Env = os.Environ()

	ct := &mcp.CommandTransport{Command: cmd}
	cli := mcp.NewClient(&mcp.Implementation{Name: "handshake", Version: "0"}, nil)
	sess, err := cli.Connect(ctx, ct, nil)
	if err != nil {
		return fmt.Errorf("connect: %w", err)
	}
	defer sess.Close()

	fmt.Println("initialize: ok")

	tools, err := sess.ListTools(ctx, nil)
	if err != nil {
		return fmt.Errorf("tools/list: %w", err)
	}
	type named struct {
		Name        string
		Description string
	}
	var list []named
	for _, t := range tools.Tools {
		list = append(list, named{Name: t.Name, Description: t.Description})
		fmt.Printf("tool: %s — %s\n", t.Name, t.Description)
	}
	// collectToolNames expects a concrete slice type — map manually
	names := make([]string, 0, len(list))
	for _, t := range list {
		names = append(names, t.Name)
	}
	out, err := formatToolsJSON(names)
	if err != nil {
		return err
	}
	fmt.Println(out)

	if miss := missingRequired(names, requiredMCPTools()); len(miss) > 0 {
		return fmt.Errorf("%s", reportMissing(miss))
	}
	fmt.Println("expanded tools: ok")
	return nil
}
