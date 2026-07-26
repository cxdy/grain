package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Client is a host-side client for the guest grain-agent HTTP API.
type Client struct {
	// BaseURL is the agent root, e.g. "http://127.0.0.1:HOSTPORT".
	BaseURL string
	// HTTP is the underlying client; if nil, http.DefaultClient is used.
	HTTP *http.Client
}

func (c *Client) http() *http.Client {
	if c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 30 * time.Second}
}

func (c *Client) base() string {
	return strings.TrimRight(c.BaseURL, "/")
}

// Health performs GET /health and decodes the Health body.
func (c *Client) Health(ctx context.Context) (*Health, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/health", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("health: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var h Health
	if err := json.NewDecoder(res.Body).Decode(&h); err != nil {
		return nil, fmt.Errorf("health decode: %w", err)
	}
	return &h, nil
}

// HeadHealth performs HEAD /health. Returns nil if the agent is up (2xx).
func (c *Client) HeadHealth(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, c.base()+"/health", nil)
	if err != nil {
		return err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("head health: status %d", res.StatusCode)
	}
	return nil
}

// ExecBuffered runs cmd with args via POST /exec?buffered=true and returns the result.
func (c *Client) ExecBuffered(ctx context.Context, cmd string, args ...string) (*ExecResult, error) {
	return c.ExecBufferedOpts(ctx, ExecOpts{Cmd: cmd, Args: args})
}

// ExecOpts holds optional parameters for a buffered exec.
type ExecOpts struct {
	Cmd  string
	Args []string
	UID  *uint32
	GID  *uint32
	Cwd  string
}

// ExecBufferedOpts is like ExecBuffered with uid/gid/cwd support.
func (c *Client) ExecBufferedOpts(ctx context.Context, opts ExecOpts) (*ExecResult, error) {
	if opts.Cmd == "" {
		return nil, fmt.Errorf("cmd is required")
	}
	u, err := url.Parse(c.base() + "/exec")
	if err != nil {
		return nil, err
	}
	q := u.Query()
	q.Set("cmd", opts.Cmd)
	for _, a := range opts.Args {
		q.Add("args", a)
	}
	q.Set("buffered", "true")
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
		return nil, err
	}
	// Exec can run longer than the default client timeout; rely on ctx.
	httpClient := c.http()
	if httpClient.Timeout > 0 {
		clone := *httpClient
		clone.Timeout = 0
		httpClient = &clone
	}
	res, err := httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
	}
	if res.StatusCode == http.StatusNotImplemented {
		return nil, fmt.Errorf("exec: streaming not implemented")
	}
	var result ExecResult
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, fmt.Errorf("exec decode (status %d): %w: %s", res.StatusCode, err, strings.TrimSpace(string(body)))
	}
	if res.StatusCode != http.StatusOK && result.Error == "" {
		result.Error = fmt.Sprintf("status %d", res.StatusCode)
	}
	return &result, nil
}
