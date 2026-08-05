package api

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/secrets"
	"github.com/cxdy/grain/internal/vm"
)

// CreateRequest is the JSON body for POST /vms.
// Wait and Timeout are sent as query parameters (not JSON body).
type CreateRequest struct {
	Name           string             `json:"name,omitempty"`
	Persistent     bool               `json:"persistent"`
	CPUs           int                `json:"cpus,omitempty"`
	MemoryMB       int                `json:"memory_mb,omitempty"`
	DiskGB         int                `json:"disk_gb,omitempty"`
	Image          string             `json:"image,omitempty"`
	Arch           string             `json:"arch,omitempty"`
	GPU            string             `json:"gpu,omitempty"`
	Network        string             `json:"network,omitempty"`
	Tags           map[string]string  `json:"tags,omitempty"`
	Userdata       string             `json:"userdata,omitempty"`
	Forwards       []vm.PortForward   `json:"forwards,omitempty"`
	Mounts         []vm.Mount         `json:"mounts,omitempty"`
	SocketForwards []vm.SocketForward `json:"socket_forwards,omitempty"`
	// Wait is auto|ssh|agent|userdata|bootstrap (empty = daemon auto; true/1 → ssh).
	Wait string `json:"-"`
	// Timeout is an optional Go duration string for create readiness (e.g. "30s").
	Timeout string `json:"-"`
}

// createVMsURL builds POST /vms with stream, wait, and timeout query params.
func createVMsURL(base string, stream bool, req CreateRequest) (string, error) {
	u, err := url.Parse(base + "/vms")
	if err != nil {
		return "", err
	}
	q := u.Query()
	if stream {
		q.Set("stream", "1")
	}
	if req.Wait != "" {
		q.Set("wait", req.Wait)
	}
	if req.Timeout != "" {
		q.Set("timeout", req.Timeout)
	}
	u.RawQuery = q.Encode()
	return u.String(), nil
}

// Create launches a VM via the daemon API (blocking JSON response).
func (c *Client) Create(ctx context.Context, req CreateRequest) (*vm.Instance, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	endpoint, err := createVMsURL(c.Base, false, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// CreateStream POSTs /vms?stream=1 and reads NDJSON CreateEvent lines.
// onEvent is called for each event (may be nil). Returns the instance from the ready event.
func (c *Client) CreateStream(ctx context.Context, req CreateRequest, onEvent func(vm.CreateEvent)) (*vm.Instance, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	endpoint, err := createVMsURL(c.Base, true, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}

	var inst *vm.Instance
	var streamErr error
	sc := bufio.NewScanner(res.Body)
	// allow large instance JSON lines
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev vm.CreateEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("decode create event: %w", err)
		}
		if onEvent != nil {
			onEvent(ev)
		}
		switch ev.Phase {
		case vm.PhaseReady:
			if ev.Instance != nil {
				inst = ev.Instance
			} else if ev.Name != "" {
				inst = &vm.Instance{Name: ev.Name, Status: vm.StatusRunning, SSHPort: ev.SSHPort}
			}
		case vm.PhaseError:
			msg := ev.Error
			if msg == "" {
				msg = ev.Message
			}
			if msg == "" {
				msg = "create failed"
			}
			streamErr = errors.New(msg)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}
	if streamErr != nil {
		return nil, streamErr
	}
	if inst == nil {
		return nil, errors.New("create stream ended without ready event")
	}
	return inst, nil
}

// Get returns a single VM by name.
func (c *Client) Get(ctx context.Context, name string) (*vm.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/vms/"+name, nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Start boots a stopped VM.
func (c *Client) Start(ctx context.Context, name string) (*vm.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+name+"/start", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// CloneRequest is the JSON body for POST /vms/{name}/clone.
type CloneRequest struct {
	// Name is the destination VM name (empty = daemon auto sbox-N).
	Name string `json:"name,omitempty"`
}

// Clone copies a stopped persistent VM disk to a new name (offline clone).
// Returns the new instance with status=stopped.
func (c *Client) Clone(ctx context.Context, src string, req CloneRequest) (*vm.Instance, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(src)+"/clone", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Shutdown stops a VM (ephemeral is deleted; persistent is left stopped).
func (c *Client) Shutdown(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+name+"/shutdown", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Pause freezes guest vCPUs (QMP stop).
func (c *Client) Pause(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/pause", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Resume continues a paused VM (QMP cont).
func (c *Client) Resume(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/resume", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Suspend stops a persistent VM (frees host RAM; optional qcow2 savevm snapshot).
func (c *Client) Suspend(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/suspend", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Restore boots a suspended VM (loadvm when a suspend snapshot exists).
func (c *Client) Restore(ctx context.Context, name string) (*vm.Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/restore", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// AddForwardRequest is the JSON body for POST /vms/{name}/forwards.
type AddForwardRequest struct {
	HostPort  int `json:"host_port"`
	GuestPort int `json:"guest_port"`
}

// AddForward starts a live SSH local port forward on a running VM.
func (c *Client) AddForward(ctx context.Context, name string, hostPort, guestPort int) (*vm.LiveForward, error) {
	b, err := json.Marshal(AddForwardRequest{HostPort: hostPort, GuestPort: guestPort})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/forwards", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var lf vm.LiveForward
	if err := json.NewDecoder(res.Body).Decode(&lf); err != nil {
		return nil, err
	}
	return &lf, nil
}

// RemoveForward stops a live SSH local forward by host port.
func (c *Client) RemoveForward(ctx context.Context, name string, hostPort int) error {
	u := fmt.Sprintf("%s/vms/%s/forwards/%d", c.Base, url.PathEscape(name), hostPort)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u, nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Exec runs a command in the guest via the daemon → grain-agent path.
// Non-zero remote exit codes are returned in *agent.ExecResult with a nil error.
func (c *Client) Exec(ctx context.Context, name, cmd string, args ...string) (*agent.ExecResult, error) {
	if cmd == "" {
		return nil, errors.New("cmd is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/exec")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("cmd", cmd)
	for _, a := range args {
		q.Add("args", a)
	}
	q.Set("buffered", "true")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 300 {
		var e struct {
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, errors.New(e.Error)
		}
		return nil, fmt.Errorf("status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var result agent.ExecResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("exec decode: %w", err)
	}
	return &result, nil
}

// AgentHealth proxies guest grain-agent GET /health for the named VM.
func (c *Client) AgentHealth(ctx context.Context, name string) (*agent.Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/vms/"+url.PathEscape(name)+"/agent/health", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var h agent.Health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// AgentDeployResult is the JSON body for POST /vms/{name}/agent/deploy.
type AgentDeployResult struct {
	Name   string        `json:"name"`
	Binary string        `json:"binary"`
	Health *agent.Health `json:"health,omitempty"`
}

// DeployAgent asks the daemon to SCP/install grain-agent into the guest over SSH.
// The agent binary must exist on the daemon host (not the remote CLI machine).
func (c *Client) DeployAgent(ctx context.Context, name string) (*AgentDeployResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/agent/deploy", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var out AgentDeployResult
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// Stats proxies guest grain-agent GET /stats for the named VM.
func (c *Client) Stats(ctx context.Context, name string) (*agent.Stats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/vms/"+url.PathEscape(name)+"/stats", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var st agent.Stats
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// --- secrets ---------------------------------------------------------------

// ListSecrets returns host secret metadata (no payloads).
func (c *Client) ListSecrets(ctx context.Context) ([]secrets.Meta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.Base+"/secrets", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var list []secrets.Meta
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

// SetSecret creates or replaces a host secret.
func (c *Client) SetSecret(ctx context.Context, req secrets.PutRequest) (*secrets.Meta, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/secrets", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var m secrets.Meta
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteSecret removes a host secret by name.
func (c *Client) DeleteSecret(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.Base+"/secrets/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// InjectSecret materializes a host secret into a running VM.
func (c *Client) InjectSecret(ctx context.Context, vmName, secretName, guestPath string) (*agent.MaterializeSecretResponse, error) {
	var body io.Reader
	if guestPath != "" {
		b, err := json.Marshal(map[string]string{"path": guestPath})
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	u := fmt.Sprintf("%s/vms/%s/secrets/%s", c.Base, url.PathEscape(vmName), url.PathEscape(secretName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var out agent.MaterializeSecretResponse
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return &out, nil
}

// ExecStream runs a command via the daemon with buffered=false and calls onFrame
// for each NDJSON ExecFrame. Returns the final exit code from the exit frame.
func (c *Client) ExecStream(ctx context.Context, name string, opts agent.ExecOpts, onFrame func(agent.ExecFrame) error) (exitCode int, err error) {
	if opts.Cmd == "" {
		return -1, errors.New("cmd is required")
	}
	if onFrame == nil {
		return -1, errors.New("onFrame is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/exec")
	if err != nil {
		return -1, err
	}
	q := u.Query()
	q.Set("cmd", opts.Cmd)
	for _, a := range opts.Args {
		q.Add("args", a)
	}
	q.Set("buffered", "false")
	if opts.UID != nil {
		q.Set("uid", fmt.Sprintf("%d", *opts.UID))
	}
	if opts.GID != nil {
		q.Set("gid", fmt.Sprintf("%d", *opts.GID))
	}
	if opts.Cwd != "" {
		q.Set("cwd", opts.Cwd)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), nil)
	if err != nil {
		return -1, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return -1, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return -1, decodeAPIError(res)
	}

	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 64*1024), 16<<20)

	gotExit := false
	code := -1
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var frame agent.ExecFrame
		if err := json.Unmarshal([]byte(line), &frame); err != nil {
			return -1, fmt.Errorf("exec stream frame decode: %w: %s", err, line)
		}
		if err := onFrame(frame); err != nil {
			return -1, err
		}
		switch frame.Type {
		case "exit":
			gotExit = true
			if frame.ExitCode != nil {
				code = *frame.ExitCode
			}
		case "error":
			msg := frame.Error
			if msg == "" {
				msg = "exec stream error"
			}
			return -1, errors.New(msg)
		}
	}
	if err := sc.Err(); err != nil && !errors.Is(err, io.EOF) {
		return -1, err
	}
	if !gotExit {
		return -1, errors.New("exec stream ended without exit frame")
	}
	return code, nil
}

// PutFile uploads raw bytes to guestPath on the named VM via PUT /vms/{name}/cp.
func (c *Client) PutFile(ctx context.Context, name, guestPath string, r io.Reader, size int64, opts agent.CPOpts) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/cp")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("path", guestPath)
	q.Set("mode", "binary")
	if opts.UID != nil {
		q.Set("uid", fmt.Sprintf("%d", *opts.UID))
	}
	if opts.GID != nil {
		q.Set("gid", fmt.Sprintf("%d", *opts.GID))
	}
	if opts.Mode != "" {
		q.Set("permissions", opts.Mode)
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = size
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// GetFile downloads guestPath from the named VM into w.
func (c *Client) GetFile(ctx context.Context, name, guestPath string, w io.Writer) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/cp")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("path", guestPath)
	q.Set("mode", "binary")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	_, err = io.Copy(w, res.Body)
	return err
}

// PutTar extracts a tar stream at guestPath on the named VM.
func (c *Client) PutTar(ctx context.Context, name, guestPath string, r io.Reader) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/cp")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("path", guestPath)
	q.Set("mode", "tar")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPut, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-tar")
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// GetTar downloads guestPath as a tar stream from the named VM into w.
func (c *Client) GetTar(ctx context.Context, name, guestPath string, w io.Writer) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/cp")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("path", guestPath)
	q.Set("mode", "tar")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	_, err = io.Copy(w, res.Body)
	return err
}

// ReadDir lists directory entries at guestPath on the named VM.
func (c *Client) ReadDir(ctx context.Context, name, guestPath string) ([]agent.FSInfo, error) {
	if guestPath == "" {
		return nil, errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/fs/readdir")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("path", guestPath)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var out []agent.FSInfo
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("readdir decode: %w", err)
	}
	return out, nil
}

// Stat returns metadata for guestPath on the named VM.
func (c *Client) Stat(ctx context.Context, name, guestPath string) (*agent.FSInfo, error) {
	if guestPath == "" {
		return nil, errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/fs/stat")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("path", guestPath)
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var info agent.FSInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("stat decode: %w", err)
	}
	return &info, nil
}

// Mkdir creates a directory on the named VM via POST /vms/{name}/fs/mkdir.
func (c *Client) Mkdir(ctx context.Context, name, guestPath string, recursive bool, mode string) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	body, err := json.Marshal(agent.MkdirRequest{
		Path:      guestPath,
		Recursive: recursive,
		Mode:      mode,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms/"+url.PathEscape(name)+"/fs/mkdir", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Remove deletes guestPath on the named VM. If recursive, uses RemoveAll.
func (c *Client) Remove(ctx context.Context, name, guestPath string, recursive bool) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.Base + "/vms/" + url.PathEscape(name) + "/fs/remove")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("path", guestPath)
	if recursive {
		q.Set("recursive", "true")
	}
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, u.String(), nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

func decodeAPIError(res *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	_ = json.NewDecoder(res.Body).Decode(&e)
	if e.Error == "" {
		return fmt.Errorf("status %d", res.StatusCode)
	}
	return errors.New(e.Error)
}
