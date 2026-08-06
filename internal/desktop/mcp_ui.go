package desktop

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// MCPStatus is the Desktop MCP page model.
type MCPStatus struct {
	Enabled   bool   `json:"enabled"`
	Listen    string `json:"listen"`
	Listening bool   `json:"listening"`
	Message   string `json:"message"`
	Local     bool   `json:"local"`
	// Snippets for IDE setup
	CursorSnippet  string `json:"cursor_snippet"`
	ClaudeSnippet  string `json:"claude_snippet"`
	GenericSnippet string `json:"generic_snippet"`
}

// GetMCPStatus reads config and optionally probes the listen address.
func GetMCPStatus(configPath string, cfg Config, activeIsLocal bool) (MCPStatus, error) {
	st := MCPStatus{Local: activeIsLocal}
	// Load raw yaml for mcp section (desktop Config may not embed full MCP)
	path := configPath
	if path == "" {
		home, _ := os.UserHomeDir()
		path = filepath.Join(home, ".grain", "config.yaml")
	}
	enabled, listen, err := readMCPFromFile(path)
	if err != nil && !os.IsNotExist(err) {
		return st, err
	}
	if listen == "" {
		listen = "127.0.0.1:7476"
	}
	st.Enabled = enabled
	st.Listen = listen
	st.CursorSnippet = mcpCursorSnippet(listen)
	st.ClaudeSnippet = mcpClaudeSnippet(listen)
	st.GenericSnippet = mcpGenericSnippet(listen)

	if !activeIsLocal {
		st.Message = "Remote host — Desktop cannot start MCP on the remote machine"
		st.Listening = false
		return st, nil
	}

	if !enabled {
		st.Message = "MCP disabled in config"
		return st, nil
	}

	// Probe TCP listen (daemon MCP or grain mcp)
	host := listen
	if !strings.Contains(host, "://") {
		// host:port
		conn, err := net.DialTimeout("tcp", normalizeListen(host), 400*time.Millisecond)
		if err != nil {
			st.Listening = false
			st.Message = "not listening — start with: grain up --mcp  or  grain mcp"
			return st, nil
		}
		_ = conn.Close()
		st.Listening = true
		st.Message = "listening on " + host
		return st, nil
	}
	st.Message = "configured"
	return st, nil
}

func normalizeListen(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "127.0.0.1:7476"
	}
	if strings.HasPrefix(s, ":") {
		return "127.0.0.1" + s
	}
	// strip path suffix host:port/path
	if i := strings.Index(s, "/"); i > 0 {
		s = s[:i]
	}
	return s
}

func readMCPFromFile(path string) (enabled bool, listen string, err error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return false, "", err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(b, &doc); err != nil {
		return false, "", err
	}
	raw, ok := doc["mcp"]
	if !ok || raw == nil {
		return false, "127.0.0.1:7476", nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return false, "127.0.0.1:7476", nil
	}
	if v, ok := m["enabled"].(bool); ok {
		enabled = v
	}
	if v, ok := m["listen"].(string); ok {
		listen = v
	}
	if listen == "" {
		listen = "127.0.0.1:7476"
	}
	return enabled, listen, nil
}

// SetMCPEnabled updates mcp.enabled (and optional listen) in config.yaml.
func SetMCPEnabled(configPath string, enabled bool, listen string) error {
	if configPath == "" {
		home, _ := os.UserHomeDir()
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	b, err := os.ReadFile(configPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	var doc map[string]interface{}
	if len(b) > 0 {
		if err := yaml.Unmarshal(b, &doc); err != nil {
			return err
		}
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	mcp, _ := doc["mcp"].(map[string]interface{})
	if mcp == nil {
		mcp = map[string]interface{}{}
	}
	mcp["enabled"] = enabled
	if strings.TrimSpace(listen) != "" {
		mcp["listen"] = strings.TrimSpace(listen)
	} else if _, ok := mcp["listen"]; !ok {
		mcp["listen"] = "127.0.0.1:7476"
	}
	doc["mcp"] = mcp
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(configPath), 0o700); err != nil {
		return err
	}
	return os.WriteFile(configPath, out, 0o600)
}

func mcpCursorSnippet(listen string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "grain": {
      "url": "http://%s/mcp"
    }
  }
}`, normalizeListen(listen))
}

func mcpClaudeSnippet(listen string) string {
	return fmt.Sprintf(`{
  "mcpServers": {
    "grain": {
      "type": "http",
      "url": "http://%s/mcp"
    }
  }
}`, normalizeListen(listen))
}

func mcpGenericSnippet(listen string) string {
	return fmt.Sprintf("# Grain MCP\n# Ensure: grain up --mcp   or   grain mcp\n# Endpoint: http://%s/mcp\n", normalizeListen(listen))
}
