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
		var e struct {
			Error string `json:"error"`
		}
		_ = json.NewDecoder(res.Body).Decode(&e)
		if e.Error == "" {
			e.Error = fmt.Sprintf("status %d", res.StatusCode)
		}
		return nil, errors.New(e.Error)
	}
	var inst vm.Instance
	if err := json.NewDecoder(res.Body).Decode(&inst); err != nil {
		return nil, err
	}
	return &inst, nil
}
