// Package recipe loads portable sandbox recipe YAML files and compiles them
// into create options plus cloud-init that implements the readiness protocol.
//
// Schema (grain/v1):
//
//	apiVersion: grain/v1
//	kind: Sandbox
//	metadata:
//	  name: node-dev
//	spec:
//	  image: grain-ubuntu
//	  bootstrap:
//	    steps:
//	      - name: packages
//	        run: apt-get install -y git
package recipe

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/cloudinit"
	"github.com/cxdy/grain/internal/vm"
	"gopkg.in/yaml.v3"
)

const (
	APIVersion  = "grain/v1"
	KindSandbox = "Sandbox"
)

// File is the on-disk recipe document.
type File struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   Metadata `yaml:"metadata"`
	Spec       Spec     `yaml:"spec"`

	// SourcePath is set by Load (absolute path to the recipe file).
	SourcePath string `yaml:"-"`
	// BaseDir is the directory containing the recipe (for relative paths).
	BaseDir string `yaml:"-"`
}

// Metadata names the sandbox defaults.
type Metadata struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description,omitempty"`
}

// Spec is create + optional bootstrap.
type Spec struct {
	Image      string `yaml:"image,omitempty"`
	CPUs       int    `yaml:"cpus,omitempty"`
	MemoryMB   int    `yaml:"memory_mb,omitempty"`
	DiskGB     int    `yaml:"disk_gb,omitempty"`
	Persistent bool   `yaml:"persistent,omitempty"`
	Arch       string `yaml:"arch,omitempty"`
	GPU        string `yaml:"gpu,omitempty"`
	Network    string `yaml:"network,omitempty"`
	// Preset merges an embedded cloud-init fragment (docker|k3s|act).
	Preset string `yaml:"preset,omitempty"`
	// Wait is auto|ssh|agent|userdata|bootstrap. Empty defaults to bootstrap
	// when Bootstrap.Steps is non-empty, otherwise empty (daemon auto).
	Wait string `yaml:"wait,omitempty"`
	// ReadyTimeout is a Go duration string (e.g. "10m") for create wait.
	ReadyTimeout string `yaml:"ready_timeout,omitempty"`

	Mounts         []Mount         `yaml:"mounts,omitempty"`
	Forwards       []Forward       `yaml:"forwards,omitempty"`
	SocketForwards []SocketForward `yaml:"socket_forwards,omitempty"`

	// Userdata is inline shell or #cloud-config (merged before generated bootstrap).
	Userdata string `yaml:"userdata,omitempty"`
	// UserdataFile is a path relative to the recipe file (or absolute).
	UserdataFile string `yaml:"userdata_file,omitempty"`

	Bootstrap Bootstrap `yaml:"bootstrap,omitempty"`
}

// Mount is host→guest 9p share.
type Mount struct {
	Host  string `yaml:"host"`
	Guest string `yaml:"guest"`
	Tag   string `yaml:"tag,omitempty"`
}

// Forward is host→guest port map (host 0 = allocate).
type Forward struct {
	HostPort  int    `yaml:"host_port,omitempty"`
	GuestPort int    `yaml:"guest_port"`
	Proto     string `yaml:"proto,omitempty"`
}

// SocketForward is host unix → guest unix via SSH streamlocal.
type SocketForward struct {
	HostPath  string `yaml:"host_path"`
	GuestPath string `yaml:"guest_path"`
}

// Bootstrap drives readiness protocol steps.
type Bootstrap struct {
	// ReadyName is written to readiness/ready_name on success.
	// Default: metadata.name.
	ReadyName string `yaml:"ready_name,omitempty"`
	// Steps run in order inside the guest (cloud-init runcmd).
	Steps []Step `yaml:"steps,omitempty"`
}

// Step is one bootstrap phase.
type Step struct {
	// Name becomes readiness phase (required, shell-safe short id).
	Name string `yaml:"name"`
	// Message is optional human status for grain status / create progress.
	Message string `yaml:"message,omitempty"`
	// Run is a shell script body (run with bash -euo pipefail).
	Run string `yaml:"run"`
}

// Compiled is the resolved create payload for the CLI/API.
type Compiled struct {
	Name           string
	Description    string
	Persistent     bool
	CPUs           int
	MemoryMB       int
	DiskGB         int
	Image          string
	Arch           string
	GPU            string
	Network        string
	Preset         string
	Wait           string
	Timeout        string // Go duration for API timeout query param
	Userdata       string
	Forwards       []vm.PortForward
	Mounts         []vm.Mount
	SocketForwards []vm.SocketForward
	Tags           map[string]string
	// HasBootstrap is true when steps were compiled into userdata.
	HasBootstrap bool
	ReadyName    string
}

// Load reads and validates a recipe file from disk.
func Load(path string) (*File, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("recipe path: %w", err)
	}
	b, err := os.ReadFile(abs)
	if err != nil {
		return nil, fmt.Errorf("read recipe: %w", err)
	}
	f, err := Parse(b)
	if err != nil {
		return nil, err
	}
	f.SourcePath = abs
	f.BaseDir = filepath.Dir(abs)
	return f, nil
}

// Parse unmarshals recipe YAML bytes (no filesystem side effects).
func Parse(data []byte) (*File, error) {
	var f File
	if err := yaml.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("parse recipe YAML: %w", err)
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	return &f, nil
}

// Validate checks schema and step constraints.
func (f *File) Validate() error {
	if f == nil {
		return fmt.Errorf("recipe is nil")
	}
	av := strings.TrimSpace(f.APIVersion)
	if av == "" {
		return fmt.Errorf("apiVersion is required (want %s)", APIVersion)
	}
	if av != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q (want %s)", av, APIVersion)
	}
	kind := strings.TrimSpace(f.Kind)
	if kind == "" {
		return fmt.Errorf("kind is required (want %s)", KindSandbox)
	}
	if !strings.EqualFold(kind, KindSandbox) {
		return fmt.Errorf("unsupported kind %q (want %s)", kind, KindSandbox)
	}

	for i, s := range f.Spec.Bootstrap.Steps {
		name := strings.TrimSpace(s.Name)
		if name == "" {
			return fmt.Errorf("bootstrap.steps[%d]: name is required", i)
		}
		if strings.ContainsAny(name, " \t\n/\\\"'") {
			return fmt.Errorf("bootstrap.steps[%d]: name %q must be a single token (no spaces/quotes)", i, name)
		}
		if strings.TrimSpace(s.Run) == "" {
			return fmt.Errorf("bootstrap.steps[%d] (%s): run is required", i, name)
		}
	}

	if w := strings.TrimSpace(f.Spec.Wait); w != "" {
		switch strings.ToLower(w) {
		case "auto", "ssh", "agent", "userdata", "bootstrap":
		default:
			return fmt.Errorf("spec.wait %q invalid (want auto|ssh|agent|userdata|bootstrap)", w)
		}
	}
	if t := strings.TrimSpace(f.Spec.ReadyTimeout); t != "" {
		if _, err := time.ParseDuration(t); err != nil {
			return fmt.Errorf("spec.ready_timeout: %w", err)
		}
	}

	for i, m := range f.Spec.Mounts {
		if strings.TrimSpace(m.Host) == "" || strings.TrimSpace(m.Guest) == "" {
			return fmt.Errorf("mounts[%d]: host and guest are required", i)
		}
	}
	for i, fwd := range f.Spec.Forwards {
		if fwd.GuestPort <= 0 {
			return fmt.Errorf("forwards[%d]: guest_port must be > 0", i)
		}
	}
	for i, s := range f.Spec.SocketForwards {
		if strings.TrimSpace(s.HostPath) == "" || strings.TrimSpace(s.GuestPath) == "" {
			return fmt.Errorf("socket_forwards[%d]: host_path and guest_path are required", i)
		}
	}
	if f.Spec.Userdata != "" && f.Spec.UserdataFile != "" {
		return fmt.Errorf("specify only one of userdata or userdata_file")
	}
	return nil
}

// Compile resolves paths, merges userdata, and builds readiness bootstrap script.
// baseDir overrides File.BaseDir when non-empty (tests).
func (f *File) Compile() (*Compiled, error) {
	if err := f.Validate(); err != nil {
		return nil, err
	}
	base := f.BaseDir
	if base == "" && f.SourcePath != "" {
		base = filepath.Dir(f.SourcePath)
	}

	c := &Compiled{
		Name:        strings.TrimSpace(f.Metadata.Name),
		Description: strings.TrimSpace(f.Metadata.Description),
		Persistent:  f.Spec.Persistent,
		CPUs:        f.Spec.CPUs,
		MemoryMB:    f.Spec.MemoryMB,
		DiskGB:      f.Spec.DiskGB,
		Image:       strings.TrimSpace(f.Spec.Image),
		Arch:        strings.TrimSpace(f.Spec.Arch),
		GPU:         strings.TrimSpace(f.Spec.GPU),
		Network:     strings.TrimSpace(f.Spec.Network),
		Preset:      strings.TrimSpace(f.Spec.Preset),
		Timeout:     strings.TrimSpace(f.Spec.ReadyTimeout),
		Tags:        map[string]string{"recipe": "true"},
	}
	if c.Name != "" {
		c.Tags["recipe_name"] = c.Name
	}

	// Mounts: "." / "./" = process CWD (project you ran grain from).
	// Other relative paths are relative to the recipe file directory.
	for _, m := range f.Spec.Mounts {
		host, err := resolveHostPath(strings.TrimSpace(m.Host), base)
		if err != nil {
			return nil, fmt.Errorf("mount host %q: %w", m.Host, err)
		}
		c.Mounts = append(c.Mounts, vm.Mount{
			Host:  host,
			Guest: strings.TrimSpace(m.Guest),
			Tag:   strings.TrimSpace(m.Tag),
		})
	}
	for _, fwd := range f.Spec.Forwards {
		proto := strings.TrimSpace(fwd.Proto)
		if proto == "" {
			proto = "tcp"
		}
		c.Forwards = append(c.Forwards, vm.PortForward{
			HostPort:  fwd.HostPort,
			GuestPort: fwd.GuestPort,
			Proto:     proto,
		})
	}
	for _, s := range f.Spec.SocketForwards {
		hp, err := resolveHostPath(strings.TrimSpace(s.HostPath), base)
		if err != nil {
			return nil, fmt.Errorf("socket_forward host_path %q: %w", s.HostPath, err)
		}
		c.SocketForwards = append(c.SocketForwards, vm.SocketForward{
			HostPath:  hp,
			GuestPath: strings.TrimSpace(s.GuestPath),
		})
	}

	// Userdata from inline or file.
	userdata := strings.TrimSpace(f.Spec.Userdata)
	if f.Spec.UserdataFile != "" {
		p := f.Spec.UserdataFile
		if !filepath.IsAbs(p) {
			p = filepath.Join(base, p)
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("userdata_file: %w", err)
		}
		userdata = string(b)
	}

	readyName := strings.TrimSpace(f.Spec.Bootstrap.ReadyName)
	if readyName == "" {
		readyName = c.Name
	}
	c.ReadyName = readyName

	steps := f.Spec.Bootstrap.Steps
	c.HasBootstrap = len(steps) > 0

	wait := strings.ToLower(strings.TrimSpace(f.Spec.Wait))
	if wait == "" && c.HasBootstrap {
		wait = vm.WaitBootstrap
	}
	c.Wait = wait

	if c.HasBootstrap {
		bootUD := BuildBootstrapUserdata(readyName, steps)
		if strings.TrimSpace(userdata) == "" {
			userdata = bootUD
		} else {
			// User/extra cloud-config first (packages, write_files), then bootstrap
			// runcmd so readiness steps run after declared package installs when both
			// are used. MergeUserData appends extra runcmd after base.
			merged, err := cloudinit.MergeUserData(userdata, bootUD)
			if err != nil {
				return nil, fmt.Errorf("merge recipe userdata with bootstrap: %w", err)
			}
			userdata = merged
		}
	}
	c.Userdata = userdata
	return c, nil
}

// BuildBootstrapUserdata returns #cloud-config that runs steps and stamps readiness.
func BuildBootstrapUserdata(readyName string, steps []Step) string {
	script := buildBootstrapScript(readyName, steps)
	// Single runcmd multi-line string; cloud-init runs with sh -c.
	doc := map[string]any{
		"runcmd": []any{script},
	}
	out, err := marshalSimpleCloudConfig(doc)
	if err != nil {
		// fallback — should not happen
		return "#cloud-config\nruncmd:\n  - |\n    echo bootstrap compile error\n"
	}
	return out
}

func buildBootstrapScript(readyName string, steps []Step) string {
	var b strings.Builder
	b.WriteString("#!/bin/bash\n")
	b.WriteString("set -euo pipefail\n")
	b.WriteString("dir=/var/lib/grain/readiness\n")
	b.WriteString("mkdir -p \"$dir\" /var/lib/grain\n")
	b.WriteString("grain_ready_report() {\n")
	b.WriteString("  local state=\"$1\" phase=\"${2:-}\" message=\"${3:-}\"\n")
	b.WriteString("  printf '%s\\n' \"$state\" >\"$dir/state\"\n")
	b.WriteString("  if [ -n \"$phase\" ]; then printf '%s\\n' \"$phase\" >\"$dir/phase\"; else rm -f \"$dir/phase\"; fi\n")
	b.WriteString("  if [ -n \"$message\" ]; then printf '%s\\n' \"$message\" >\"$dir/message\"; else rm -f \"$dir/message\"; fi\n")
	b.WriteString("  date -u +\"%Y-%m-%dT%H:%M:%SZ\" >\"$dir/updated_at\" 2>/dev/null || true\n")
	b.WriteString("  if [ \"$state\" = \"failed\" ] && [ -n \"$message\" ]; then printf '%s\\n' \"$message\" >\"$dir/error\"; fi\n")
	b.WriteString("  if [ \"$state\" != \"failed\" ]; then rm -f \"$dir/error\"; fi\n")
	b.WriteString("}\n")
	b.WriteString("trap 'ec=$?; grain_ready_report failed \"\" \"bootstrap exit $ec\"; exit $ec' ERR\n")
	b.WriteString("grain_ready_report running\n")

	for _, s := range steps {
		name := strings.TrimSpace(s.Name)
		msg := strings.TrimSpace(s.Message)
		if msg == "" {
			msg = name
		}
		fmt.Fprintf(&b, "grain_ready_report running %s %s\n",
			shellSingleQuote(name), shellSingleQuote(msg))
		b.WriteString("(\n")
		b.WriteString("set -euo pipefail\n")
		b.WriteString(strings.TrimRight(s.Run, "\n"))
		b.WriteString("\n)\n")
	}

	if rn := strings.TrimSpace(readyName); rn != "" {
		fmt.Fprintf(&b, "printf '%%s\\n' %s >\"$dir/ready_name\"\n", shellSingleQuote(rn))
	}
	b.WriteString("grain_ready_report ready\n")
	b.WriteString("touch /var/lib/grain/userdata-ran\n")
	b.WriteString("echo grain-ready > /var/lib/grain-ready\n")
	return b.String()
}

// resolveHostPath turns a recipe host path into an absolute path.
// "." and "./" use the process working directory; other relative paths
// are resolved against recipeBase (the directory of the recipe file).
func resolveHostPath(host, recipeBase string) (string, error) {
	if host == "" {
		return "", nil
	}
	if host == "." || host == "./" {
		return filepath.Abs(".")
	}
	if filepath.IsAbs(host) {
		return filepath.Clean(host), nil
	}
	if recipeBase == "" {
		return filepath.Abs(host)
	}
	return filepath.Abs(filepath.Join(recipeBase, host))
}

// shellSingleQuote wraps s for safe use as a single shell word.
func shellSingleQuote(s string) string {
	// 'foo'bar'baz' → 'foo'"'"'bar'"'"'baz'
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func marshalSimpleCloudConfig(m map[string]any) (string, error) {
	// Ensure runcmd multi-line script uses literal block if possible via yaml.
	var node yaml.Node
	if err := node.Encode(m); err != nil {
		return "", err
	}
	// Force literal style on the runcmd string for readability.
	forceLiteralRuncmd(&node)
	out, err := yaml.Marshal(&node)
	if err != nil {
		return "", err
	}
	return "#cloud-config\n" + string(out), nil
}

func forceLiteralRuncmd(n *yaml.Node) {
	if n == nil {
		return
	}
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			k, v := n.Content[i], n.Content[i+1]
			if k.Value == "runcmd" && v.Kind == yaml.SequenceNode {
				for _, item := range v.Content {
					if item.Kind == yaml.ScalarNode && strings.Contains(item.Value, "\n") {
						item.Style = yaml.LiteralStyle
					}
				}
			}
			forceLiteralRuncmd(v)
		}
	}
	if n.Kind == yaml.SequenceNode || n.Kind == yaml.DocumentNode {
		for _, c := range n.Content {
			forceLiteralRuncmd(c)
		}
	}
}
