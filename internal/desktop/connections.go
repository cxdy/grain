package desktop

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// AddConnection appends or updates a named connection in config.yaml.
// Preserves other YAML keys via full re-marshal of known fields + raw merge is hard;
// we load as map, update connections, write back.
func AddConnection(configPath string, conn Connection) error {
	if strings.TrimSpace(conn.Name) == "" {
		return fmt.Errorf("connection name is required")
	}
	if conn.Name == "local" {
		return fmt.Errorf("cannot overwrite the built-in local connection name")
	}
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var doc map[string]interface{}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	list, _ := doc["connections"].([]interface{})
	entry := map[string]interface{}{
		"name": conn.Name,
	}
	if conn.API != "" {
		entry["api"] = NormalizeAPIURL(conn.API)
	}
	if conn.Token != "" {
		entry["token"] = conn.Token
	}
	if conn.TokenEnv != "" {
		entry["token_env"] = conn.TokenEnv
	}
	if conn.Notes != "" {
		entry["notes"] = conn.Notes
	}
	// MCP listen is stored separately (mcp_listen key); notes may still carry free text.

	replaced := false
	for i, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		if fmt.Sprint(m["name"]) == conn.Name {
			list[i] = entry
			replaced = true
			break
		}
	}
	if !replaced {
		list = append(list, entry)
	}
	doc["connections"] = list

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o600)
}

// ConnectionWithMCP is UI payload for adding a host.
type ConnectionWithMCP struct {
	Name       string `json:"name"`
	API        string `json:"api"`
	Token      string `json:"token"`
	TokenEnv   string `json:"token_env"`
	MCPEnabled bool   `json:"mcp_enabled"`
	MCPListen  string `json:"mcp_listen"`
}

// SaveHostConnection writes a remote host profile (and MCP note) to config.
func SaveHostConnection(configPath string, h ConnectionWithMCP) error {
	if strings.TrimSpace(h.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if strings.TrimSpace(h.API) == "" {
		return fmt.Errorf("API endpoint is required")
	}
	notes := ""
	if h.MCPEnabled {
		mcp := strings.TrimSpace(h.MCPListen)
		if mcp == "" {
			return fmt.Errorf("MCP listen is required when MCP is enabled")
		}
		notes = "mcp:" + mcp
	}
	return AddConnection(configPath, Connection{
		Name:     h.Name,
		API:      h.API,
		Token:    h.Token,
		TokenEnv: h.TokenEnv,
		Notes:    notes,
	})
}

// DeleteConnection removes a named remote connection from config.yaml.
// The built-in "local" profile cannot be deleted.
func DeleteConnection(configPath, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("connection name is required")
	}
	if name == "local" {
		return fmt.Errorf("cannot delete the built-in local connection")
	}
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return fmt.Errorf("parse config: %w", err)
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	list, _ := doc["connections"].([]interface{})
	var next []interface{}
	found := false
	for _, item := range list {
		m, ok := item.(map[string]interface{})
		if !ok {
			next = append(next, item)
			continue
		}
		if fmt.Sprint(m["name"]) == name {
			found = true
			continue
		}
		next = append(next, item)
	}
	if !found {
		return fmt.Errorf("connection %q not found", name)
	}
	doc["connections"] = next
	// If desktop.default_connection pointed at the deleted host, reset to local.
	if desk, ok := doc["desktop"].(map[string]interface{}); ok {
		if fmt.Sprint(desk["default_connection"]) == name {
			desk["default_connection"] = "local"
			doc["desktop"] = desk
		}
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o600)
}

// SettingsForm is the common Settings form payload (not raw YAML).
type SettingsForm struct {
	DefaultConnection string `json:"default_connection"`
	StartLocalDaemon  bool   `json:"start_local_daemon"`
	DataDir           string `json:"data_dir"`
	API               string `json:"api"`
	APIURL            string `json:"api_url"`
}

// SaveSettingsForm merges common preference fields into config.yaml.
func SaveSettingsForm(configPath string, form SettingsForm) error {
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var doc map[string]interface{}
	if len(raw) > 0 {
		if err := yaml.Unmarshal(raw, &doc); err != nil {
			return fmt.Errorf("parse config: %w", err)
		}
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	if strings.TrimSpace(form.DataDir) != "" {
		doc["data_dir"] = strings.TrimSpace(form.DataDir)
	}
	if strings.TrimSpace(form.API) != "" {
		doc["api"] = strings.TrimSpace(form.API)
	}
	// Allow clearing api_url with empty string when key was set intentionally —
	// only write when non-empty to avoid wiping.
	if strings.TrimSpace(form.APIURL) != "" {
		doc["api_url"] = NormalizeAPIURL(form.APIURL)
	}
	desk, _ := doc["desktop"].(map[string]interface{})
	if desk == nil {
		desk = map[string]interface{}{}
	}
	def := strings.TrimSpace(form.DefaultConnection)
	if def == "" {
		def = "local"
	}
	desk["default_connection"] = def
	desk["start_local_daemon"] = form.StartLocalDaemon
	doc["desktop"] = desk

	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	if !strings.HasSuffix(string(out), "\n") {
		out = append(out, '\n')
	}
	return os.WriteFile(configPath, out, 0o600)
}
