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
	"github.com/cxdy/grain/internal/vm"
)

// CreateRequest is the JSON body for POST /vms.
type CreateRequest struct {
	Name       string            `json:"name,omitempty"`
	Persistent bool              `json:"persistent"`
	CPUs       int               `json:"cpus,omitempty"`
	MemoryMB   int               `json:"memory_mb,omitempty"`
	DiskGB     int               `json:"disk_gb,omitempty"`
	Image      string            `json:"image,omitempty"`
	Tags       map[string]string `json:"tags,omitempty"`
	Userdata   string            `json:"userdata,omitempty"`
	Forwards   []vm.PortForward  `json:"forwards,omitempty"`
	Mounts     []vm.Mount        `json:"mounts,omitempty"`
}

// Create launches a VM via the daemon API (blocking JSON response).
func (c *Client) Create(ctx context.Context, req CreateRequest) (*vm.Instance, error) {
	b, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
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
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+"/vms?stream=1", bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "application/x-ndjson")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
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
	defer res.Body.Close()
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
	defer res.Body.Close()
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
	defer res.Body.Close()
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
	defer res.Body.Close()
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
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		return nil, decodeAPIError(res)
	}
	var h agent.Health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, err
	}
	return &h, nil
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
