package api

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"

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
}

// Create launches a VM via the daemon API.
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
