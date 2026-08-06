package desktop

import (
	"bytes"
	"context"
	"crypto/rand"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/config"
	"github.com/cxdy/grain/internal/names"
	"gopkg.in/yaml.v3"
)

// ValidateSandboxName checks VM names (same rules as the daemon).
// Valid: lowercase letter, then 0–62 of [a-z0-9-], e.g. "test", "sbox-1".
func ValidateSandboxName(name string) error {
	if name == "" {
		return nil // empty = daemon auto-name
	}
	if names.Valid(name) {
		return nil
	}
	return fmt.Errorf("invalid name %q: use lowercase letters, digits, and hyphens (e.g. test or sbox-1); must start with a letter", name)
}

// ReadConfigFile returns the raw config file bytes (or a starter template if missing).
func ReadConfigFile(path string) (string, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		path = filepath.Join(home, ".grain", "config.yaml")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "# grain config\n", nil
		}
		return "", err
	}
	return string(b), nil
}

// CheckConfigContent writes content to a temp file and validates it.
// Always validates in-process first (strict unknown keys). Optionally also
// runs `grain check-config` when available for CLI parity.
// Caller should remove tmpPath when done (always returned when created).
func CheckConfigContent(content string, runner CommandRunner) (tmpPath string, err error) {
	f, err := os.CreateTemp("", "grain-config-*.yaml")
	if err != nil {
		return "", err
	}
	tmpPath = f.Name()
	if _, err := f.WriteString(content); err != nil {
		_ = f.Close()
		_ = os.Remove(tmpPath)
		return "", err
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", err
	}

	// In-process first — reliable even when PATH has an older grain binary.
	if _, vErr := config.ValidateFile(tmpPath); vErr != nil {
		return tmpPath, vErr
	}

	if runner == nil {
		runner = ExecRunner{}
	}
	if grain, lookErr := runner.LookPath("grain"); lookErr == nil {
		cmd := exec.Command(grain, "check-config", tmpPath)
		var combined bytes.Buffer
		cmd.Stdout = &combined
		cmd.Stderr = &combined
		if runErr := cmd.Run(); runErr != nil {
			msg := strings.TrimSpace(combined.String())
			// Older grain builds lack check-config — already validated in-process.
			if strings.Contains(msg, "unknown command") || strings.Contains(msg, "unknown flag") {
				return tmpPath, nil
			}
			if msg == "" {
				msg = runErr.Error()
			}
			return tmpPath, fmt.Errorf("%s", msg)
		}
	}
	return tmpPath, nil
}

// SaveConfigResult is returned after a successful save + optional daemon restart.
type SaveConfigResult struct {
	Path            string `json:"path"`
	DaemonRestarted bool   `json:"daemon_restarted"`
	Message         string `json:"message"`
}

// TokenActionResult is returned after generate/revoke of api_token.
type TokenActionResult struct {
	Token   string `json:"token,omitempty"` // only on generate, show once
	Message string `json:"message"`
	HasToken bool  `json:"has_token"`
}

// GenerateAPIToken creates a new random token, writes api_token to config, returns plaintext once.
func GenerateAPIToken(configPath string) (TokenActionResult, error) {
	var res TokenActionResult
	tok, err := randomToken(32)
	if err != nil {
		return res, err
	}
	if err := setConfigStringKey(configPath, "api_token", tok); err != nil {
		return res, err
	}
	res.Token = tok
	res.HasToken = true
	res.Message = "API token written to config — copy it now; it will not be shown again"
	return res, nil
}

// RevokeAPIToken clears api_token (and auth_token) in config.
func RevokeAPIToken(configPath string) (TokenActionResult, error) {
	var res TokenActionResult
	if err := deleteConfigKeys(configPath, "api_token", "auth_token"); err != nil {
		return res, err
	}
	res.HasToken = false
	res.Message = "API token removed from config"
	return res, nil
}

func randomToken(n int) (string, error) {
	const alphabet = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	b := make([]byte, n)
	if _, err := randRead(b); err != nil {
		return "", err
	}
	for i := range b {
		b[i] = alphabet[int(b[i])%len(alphabet)]
	}
	return string(b), nil
}

func randRead(b []byte) (int, error) {
	return rand.Read(b)
}

func setConfigStringKey(configPath, key, value string) error {
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
			return err
		}
	}
	if doc == nil {
		doc = map[string]interface{}{}
	}
	doc[key] = value
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

func deleteConfigKeys(configPath string, keys ...string) error {
	if configPath == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		configPath = filepath.Join(home, ".grain", "config.yaml")
	}
	raw, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var doc map[string]interface{}
	if err := yaml.Unmarshal(raw, &doc); err != nil {
		return err
	}
	if doc == nil {
		return nil
	}
	for _, k := range keys {
		delete(doc, k)
	}
	out, err := yaml.Marshal(doc)
	if err != nil {
		return err
	}
	if !strings.HasSuffix(string(out), "\n") {
		out = append(out, '\n')
	}
	return os.WriteFile(configPath, out, 0o600)
}

// SaveConfigFile validates content, writes path (0600), and restarts the local daemon
// when restartDaemon is true (grain down + grain up).
func SaveConfigFile(path, content string, restartDaemon bool, runner CommandRunner) (SaveConfigResult, error) {
	if path == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return SaveConfigResult{}, err
		}
		path = filepath.Join(home, ".grain", "config.yaml")
	}
	if runner == nil {
		runner = ExecRunner{}
	}

	tmp, err := CheckConfigContent(content, runner)
	if tmp != "" {
		defer func() { _ = os.Remove(tmp) }()
	}
	if err != nil {
		return SaveConfigResult{}, fmt.Errorf("check-config failed:\n%s", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return SaveConfigResult{}, err
	}
	// Ensure trailing newline (avoids shell "%" no-EOL marker); strip trailing NULs.
	content = strings.TrimRight(content, "\x00")
	if !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		return SaveConfigResult{}, err
	}

	res := SaveConfigResult{Path: path, Message: "config saved"}
	if !restartDaemon {
		return res, nil
	}
	grain, lookErr := runner.LookPath("grain")
	if lookErr != nil {
		res.Message = "config saved (grain not on PATH — restart daemon manually with: grain down && grain up)"
		return res, nil
	}
	ctx := context.Background()
	_ = runner.StartBackground(ctx, grain, "down")
	time.Sleep(400 * time.Millisecond)
	if err := runner.StartBackground(ctx, grain, "up"); err != nil {
		return res, fmt.Errorf("config saved but grain up failed: %w", err)
	}
	res.DaemonRestarted = true
	res.Message = "config saved; local daemon restarted"
	return res, nil
}
