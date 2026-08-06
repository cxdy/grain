// Package client is the public Go SDK for the grain daemon HTTP API.
//
//	c, err := client.DialUnix(filepath.Join(home, ".grain", "grain.sock"))
//	// or
//	c, err := client.DialHTTP("http://127.0.0.1:7474", os.Getenv("GRAIN_TOKEN"))
//
//	inst, err := c.Create(ctx, client.CreateRequest{Persistent: false})
//
// This package reimplements a thin HTTP client and does not import internal/
// packages. Types mirror the daemon JSON shapes documented in api/openapi.yaml.
package client

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Client talks to a running grain daemon over HTTP (TCP or Unix socket).
type Client struct {
	base  string
	token string
	http  *http.Client
}

// DialUnix connects to the daemon Unix socket (typical local CLI path).
// token may be empty when the daemon has no api_token configured.
func DialUnix(socketPath string) (*Client, error) {
	return DialUnixToken(socketPath, "")
}

// DialUnixToken is DialUnix with an optional Bearer token.
func DialUnixToken(socketPath, token string) (*Client, error) {
	if socketPath == "" {
		return nil, errors.New("socket path is required")
	}
	return &Client{
		base:  "http://grain",
		token: token,
		http: &http.Client{
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					var d net.Dialer
					return d.DialContext(ctx, "unix", socketPath)
				},
				ResponseHeaderTimeout: 5 * time.Minute,
			},
		},
	}, nil
}

// DialHTTP connects to a TCP (or other HTTP) base URL.
// token is sent as Authorization: Bearer when non-empty.
func DialHTTP(baseURL, token string) (*Client, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return nil, errors.New("base URL is required")
	}
	return &Client{
		base:  baseURL,
		token: token,
		http:  &http.Client{},
	}, nil
}

// SetToken sets or clears the Bearer token used on subsequent requests.
func (c *Client) SetToken(token string) {
	c.token = token
}

// Token returns the configured Bearer token (may be empty).
func (c *Client) Token() string {
	return c.token
}

// Base returns the HTTP base URL (e.g. http://grain or http://127.0.0.1:7474).
func (c *Client) Base() string {
	return c.base
}

func (c *Client) do(req *http.Request) (*http.Response, error) {
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	cli := c.http
	if cli == nil {
		cli = http.DefaultClient
	}
	return cli.Do(req)
}

// Health checks GET /healthz.
func (c *Client) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/healthz", nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return errors.New("unhealthy")
	}
	return nil
}

// Info returns GET /info (name + version).
func (c *Client) Info(ctx context.Context) (map[string]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/info", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// List returns all VMs.
func (c *Client) List(ctx context.Context) ([]*Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/vms", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var list []*Instance
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
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

// Create launches a VM (blocking JSON response until ready).
// Set req.Wait to auto|ssh|agent|userdata|bootstrap and req.Timeout to a Go duration
// (e.g. "3m") to control readiness; both are sent as query parameters.
func (c *Client) Create(ctx context.Context, req CreateRequest) (*Instance, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	endpoint, err := createVMsURL(c.base, false, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// CreateStream POSTs /vms?stream=1 and reads NDJSON CreateEvent lines.
// onEvent is called for each event (may be nil). Returns the instance from the ready event.
// Wait and Timeout from req are applied as query parameters (same as Create).
func (c *Client) CreateStream(ctx context.Context, req CreateRequest, onEvent func(CreateEvent)) (*Instance, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	endpoint, err := createVMsURL(c.base, true, req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")
	res, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}

	var inst *Instance
	var streamErr error
	sc := bufio.NewScanner(res.Body)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" {
			continue
		}
		var ev CreateEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			return nil, fmt.Errorf("decode create event: %w", err)
		}
		if onEvent != nil {
			onEvent(ev)
		}
		switch ev.Phase {
		case PhaseReady:
			if ev.Instance != nil {
				inst = ev.Instance
			} else if ev.Name != "" {
				inst = &Instance{Name: ev.Name, Status: StatusRunning, SSHPort: ev.SSHPort}
			}
		case PhaseError:
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
func (c *Client) Get(ctx context.Context, name string) (*Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/vms/"+url.PathEscape(name), nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Delete removes a VM.
func (c *Client) Delete(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/vms/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Start boots a stopped VM.
func (c *Client) Start(ctx context.Context, name string) (*Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/start", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Clone copies a stopped persistent VM disk to a new name (offline clone).
// dstName may be empty for daemon auto-naming. Returns status=stopped.
func (c *Client) Clone(ctx context.Context, src, dstName string) (*Instance, error) {
	body := map[string]string{}
	if dstName != "" {
		body["name"] = dstName
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(src)+"/clone", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// Stop is an alias for Shutdown.
func (c *Client) Stop(ctx context.Context, name string) error {
	return c.Shutdown(ctx, name)
}

// Shutdown stops a VM (ephemeral is deleted; persistent is left stopped).
func (c *Client) Shutdown(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/shutdown", nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Pause freezes guest vCPUs via QMP (status becomes paused).
func (c *Client) Pause(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/pause", nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Resume continues a paused VM via QMP.
func (c *Client) Resume(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/resume", nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Suspend stops a persistent VM and frees host RAM (optional qcow2 savevm).
// Differs from Pause, which freezes vCPUs while keeping QEMU running.
func (c *Client) Suspend(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/suspend", nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Restore boots a suspended VM (may load a savevm snapshot when available).
func (c *Client) Restore(ctx context.Context, name string) (*Instance, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/restore", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var inst Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}

// AddForward starts a live host→guest TCP forward on a running VM.
// hostPort 0 lets the daemon allocate a free high port. Returns the
// allocated LiveForward (including resolved host_port).
func (c *Client) AddForward(ctx context.Context, name string, hostPort, guestPort int) (*LiveForward, error) {
	body, err := json.Marshal(map[string]int{
		"host_port":  hostPort,
		"guest_port": guestPort,
	})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/forwards", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var lf LiveForward
	if err := json.NewDecoder(res.Body).Decode(&lf); err != nil {
		return nil, err
	}
	return &lf, nil
}

// RemoveForward tears down a live host forward.
func (c *Client) RemoveForward(ctx context.Context, name string, hostPort int) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.base+"/vms/"+url.PathEscape(name)+"/forwards/"+strconv.Itoa(hostPort), nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
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
// Non-zero remote exit codes are returned in *ExecResult with a nil error.
func (c *Client) Exec(ctx context.Context, name, cmd string, args ...string) (*ExecResult, error) {
	if cmd == "" {
		return nil, errors.New("cmd is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/exec")
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
	res, err := c.do(req)
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
	var result ExecResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("exec decode: %w", err)
	}
	return &result, nil
}

// ExecStream runs a command with buffered=false and calls onFrame for each NDJSON frame.
// Returns the final exit code from the exit frame.
func (c *Client) ExecStream(ctx context.Context, name string, opts ExecOpts, onFrame func(ExecFrame) error) (exitCode int, err error) {
	if opts.Cmd == "" {
		return -1, errors.New("cmd is required")
	}
	if onFrame == nil {
		return -1, errors.New("onFrame is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/exec")
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
	res, err := c.do(req)
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
		var frame ExecFrame
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

// AgentHealth proxies guest grain-agent GET /health for the named VM.
func (c *Client) AgentHealth(ctx context.Context, name string) (*Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/vms/"+url.PathEscape(name)+"/agent/health", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var h Health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// DeployAgent asks the daemon to SCP/install grain-agent into the guest over SSH.
// The agent binary must exist on the daemon host (not the remote client machine).
func (c *Client) DeployAgent(ctx context.Context, name string) (*AgentDeployResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/agent/deploy", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
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
func (c *Client) Stats(ctx context.Context, name string) (*Stats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/vms/"+url.PathEscape(name)+"/stats", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var st Stats
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		return nil, err
	}
	return &st, nil
}

// Metrics returns host-side metrics ring history for the named VM.
func (c *Client) Metrics(ctx context.Context, name string) (*MetricsHistory, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/vms/"+url.PathEscape(name)+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var h MetricsHistory
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
}

// ListSecrets returns host secret metadata.
func (c *Client) ListSecrets(ctx context.Context) ([]SecretMeta, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base+"/secrets", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var list []SecretMeta
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return nil, err
	}
	return list, nil
}

// SetSecret creates or replaces a host secret.
func (c *Client) SetSecret(ctx context.Context, req SecretPut) (*SecretMeta, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/secrets", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var m SecretMeta
	if err := json.NewDecoder(res.Body).Decode(&m); err != nil {
		return nil, err
	}
	return &m, nil
}

// DeleteSecret removes a host secret.
func (c *Client) DeleteSecret(ctx context.Context, name string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, c.base+"/secrets/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	res, err := c.do(req)
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
func (c *Client) InjectSecret(ctx context.Context, vmName, secretName, guestPath string) (map[string]string, error) {
	var body io.Reader
	if guestPath != "" {
		b, err := json.Marshal(map[string]string{"path": guestPath})
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(b)
	}
	u := fmt.Sprintf("%s/vms/%s/secrets/%s", c.base, url.PathEscape(vmName), url.PathEscape(secretName))
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var out map[string]string
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, err
	}
	return out, nil
}

// PutFile uploads raw bytes to guestPath via PUT /vms/{name}/cp.
func (c *Client) PutFile(ctx context.Context, name, guestPath string, r io.Reader, size int64, opts CPOpts) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/cp")
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
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// GetFile downloads guestPath into w.
func (c *Client) GetFile(ctx context.Context, name, guestPath string, w io.Writer) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/cp")
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
	res, err := c.do(req)
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
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/cp")
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
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// GetTar downloads guestPath as a tar stream into w.
func (c *Client) GetTar(ctx context.Context, name, guestPath string, w io.Writer) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/cp")
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
	res, err := c.do(req)
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

// ReadDir lists directory entries at guestPath.
func (c *Client) ReadDir(ctx context.Context, name, guestPath string) ([]FSInfo, error) {
	if guestPath == "" {
		return nil, errors.New("path is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/fs/readdir")
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
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var out []FSInfo
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("readdir decode: %w", err)
	}
	return out, nil
}

// Stat returns metadata for guestPath.
func (c *Client) Stat(ctx context.Context, name, guestPath string) (*FSInfo, error) {
	if guestPath == "" {
		return nil, errors.New("path is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/fs/stat")
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
	res, err := c.do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var info FSInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("stat decode: %w", err)
	}
	return &info, nil
}

// Mkdir creates a directory on the named VM.
func (c *Client) Mkdir(ctx context.Context, name, guestPath string, recursive bool, mode string) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	body, err := json.Marshal(MkdirRequest{
		Path:      guestPath,
		Recursive: recursive,
		Mode:      mode,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/fs/mkdir", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Symlink creates a symbolic link on the named VM via POST /vms/{name}/fs/symlink.
// Existing paths at guestPath are replaced.
func (c *Client) Symlink(ctx context.Context, name, guestPath, target string) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	if target == "" {
		return errors.New("target is required")
	}
	body, err := json.Marshal(SymlinkRequest{
		Path:   guestPath,
		Target: target,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base+"/vms/"+url.PathEscape(name)+"/fs/symlink", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.do(req)
	if err != nil {
		return err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode >= 300 {
		return decodeAPIError(res)
	}
	return nil
}

// Remove deletes guestPath. If recursive, uses RemoveAll semantics on the guest.
func (c *Client) Remove(ctx context.Context, name, guestPath string, recursive bool) error {
	if guestPath == "" {
		return errors.New("path is required")
	}
	u, err := url.Parse(c.base + "/vms/" + url.PathEscape(name) + "/fs/remove")
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
	res, err := c.do(req)
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
