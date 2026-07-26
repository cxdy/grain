package cloudinit

import (
	"fmt"
	"strings"

	"gopkg.in/yaml.v3"
)

// listMergeKeys are cloud-config keys whose values are sequences that should
// be appended (base then extra) rather than replaced.
var listMergeKeys = map[string]bool{
	"packages":    true,
	"runcmd":      true,
	"write_files": true,
	"users":       true,
}

// BaseUserData builds the grain-managed cloud-config document as a map.
// It injects hostname, SSH keys, the grain user, and the grain-ready marker.
func BaseUserData(hostname, sshPubLine string) map[string]any {
	key := strings.TrimSpace(sshPubLine)
	authCmd := fmt.Sprintf(
		"mkdir -p /home/ubuntu/.ssh /root/.ssh; echo '%s' >> /home/ubuntu/.ssh/authorized_keys; echo '%s' >> /root/.ssh/authorized_keys; chown -R ubuntu:ubuntu /home/ubuntu/.ssh 2>/dev/null || true; chmod 600 /home/ubuntu/.ssh/authorized_keys /root/.ssh/authorized_keys 2>/dev/null || true",
		key, key,
	)
	return map[string]any{
		"hostname":         hostname,
		"fqdn":             hostname + ".local",
		"manage_etc_hosts": true,
		"ssh_pwauth":       false,
		"disable_root":     false,
		"users": []any{
			"default",
			map[string]any{
				"name":        "grain",
				"groups":      []any{"sudo", "adm"},
				"shell":       "/bin/bash",
				"lock_passwd": true,
				"sudo":        []any{"ALL=(ALL) NOPASSWD:ALL"},
				"ssh_authorized_keys": []any{
					key,
				},
			},
		},
		"ssh_authorized_keys": []any{
			key,
		},
		"runcmd": []any{
			[]any{"sh", "-c", authCmd},
			[]any{"sh", "-c", "echo grain-ready > /var/lib/grain-ready"},
		},
	}
}

// MergeUserData merges a base #cloud-config document with extra userdata into
// one valid #cloud-config YAML document.
//
// extra handling:
//   - empty: return base unchanged (with #cloud-config header)
//   - shell / non-cloud-config: treat as a single additional runcmd entry
//   - #cloud-config YAML: merge keys; packages, runcmd, write_files, users are
//     appended; other keys from extra override base
func MergeUserData(baseCloudConfig string, extra string) (string, error) {
	baseMap, err := parseCloudConfig(baseCloudConfig)
	if err != nil {
		return "", fmt.Errorf("parse base cloud-config: %w", err)
	}
	if baseMap == nil {
		baseMap = map[string]any{}
	}

	extra = strings.TrimSpace(extra)
	if extra == "" {
		return marshalCloudConfig(baseMap)
	}

	var extraMap map[string]any
	if isCloudConfig(extra) {
		extraMap, err = parseCloudConfig(extra)
		if err != nil {
			return "", fmt.Errorf("parse extra cloud-config: %w", err)
		}
	} else {
		// Shell one-liner or script: append as runcmd entry.
		extraMap = map[string]any{
			"runcmd": []any{extra},
		}
	}

	merged := mergeCloudMaps(baseMap, extraMap)
	return marshalCloudConfig(merged)
}

// RenderUserData builds the final user-data document for a VM seed.
func RenderUserData(hostname, sshPubLine, extra string) (string, error) {
	baseYAML, err := yaml.Marshal(BaseUserData(hostname, sshPubLine))
	if err != nil {
		return "", err
	}
	return MergeUserData(string(baseYAML), extra)
}

func isCloudConfig(s string) bool {
	// Strip a leading shebang? No — #!/bin/bash is not cloud-config.
	// Only treat as cloud-config when the first non-empty line is #cloud-config
	// (optionally after a BOM or blank lines).
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		return strings.HasPrefix(line, "#cloud-config")
	}
	return false
}

func parseCloudConfig(s string) (map[string]any, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return map[string]any{}, nil
	}
	var m map[string]any
	if err := yaml.Unmarshal([]byte(s), &m); err != nil {
		return nil, err
	}
	if m == nil {
		m = map[string]any{}
	}
	return m, nil
}

func marshalCloudConfig(m map[string]any) (string, error) {
	b, err := yaml.Marshal(m)
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(b), nil
}

func mergeCloudMaps(base, extra map[string]any) map[string]any {
	out := make(map[string]any, len(base)+len(extra))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range extra {
		if listMergeKeys[k] {
			out[k] = appendAny(out[k], v)
			continue
		}
		// Scalar / mapping overrides from extra.
		out[k] = v
	}
	return out
}

func appendAny(baseVal, extraVal any) any {
	baseList := toAnySlice(baseVal)
	extraList := toAnySlice(extraVal)
	if baseList == nil && extraList == nil {
		return extraVal
	}
	out := make([]any, 0, len(baseList)+len(extraList))
	out = append(out, baseList...)
	out = append(out, extraList...)
	return out
}

func toAnySlice(v any) []any {
	if v == nil {
		return nil
	}
	switch x := v.(type) {
	case []any:
		return x
	case []string:
		out := make([]any, len(x))
		for i, s := range x {
			out[i] = s
		}
		return out
	default:
		// Single scalar treated as one-element list (defensive).
		return []any{v}
	}
}
