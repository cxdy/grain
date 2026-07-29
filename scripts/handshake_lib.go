package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// requiredMCPTools is the minimum set tools/list must expose.
func requiredMCPTools() []string {
	return []string{
		"grain_health", "grain_list_vms", "grain_get_vm", "grain_create_vm",
		"grain_start_vm", "grain_stop_vm", "grain_delete_vm", "grain_exec",
		"grain_write_file", "grain_read_file", "grain_agent_health", "grain_logs",
		"grain_stats", "grain_workspace_sandbox", "grain_forward_add", "grain_forward_remove",
		"grain_image_list", "grain_image_pull", "grain_fs_readdir", "grain_act", "grain_k3s",
	}
}

// collectToolNames extracts tool names from a tools/list result payload.
func collectToolNames(tools []struct {
	Name        string
	Description string
}) []string {
	names := make([]string, 0, len(tools))
	for _, t := range tools {
		names = append(names, t.Name)
	}
	return names
}

// missingRequired returns required tools not present in have.
func missingRequired(have []string, need []string) []string {
	set := map[string]bool{}
	for _, n := range have {
		set[n] = true
	}
	var miss []string
	for _, n := range need {
		if !set[n] {
			miss = append(miss, n)
		}
	}
	return miss
}

// formatToolsJSON builds the handshake summary JSON body.
func formatToolsJSON(names []string) (string, error) {
	b, err := json.MarshalIndent(map[string]any{
		"ok":    true,
		"count": len(names),
		"tools": names,
	}, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// pickGrainBin returns the grain binary path from args (default ./bin/grain).
func pickGrainBin(args []string) string {
	if len(args) > 1 && strings.TrimSpace(args[1]) != "" {
		return args[1]
	}
	return "./bin/grain"
}

// reportMissing formats a missing-tools error message.
func reportMissing(missing []string) string {
	return fmt.Sprintf("missing required tool %q", missing[0])
}
