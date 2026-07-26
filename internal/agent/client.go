package agent

import (
	"bufio"
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

// longHTTP returns a client with no overall Timeout so ctx controls duration
// for long-running ops (exec, streaming, large copies).
func (c *Client) longHTTP() *http.Client {
	httpClient := c.http()
	if httpClient.Timeout > 0 {
		clone := *httpClient
		clone.Timeout = 0
		return &clone
	}
	return httpClient
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
	res, err := c.longHTTP().Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, 32<<20))
	if err != nil {
		return nil, err
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

// ExecStream calls onFrame for each NDJSON frame from POST /exec?buffered=false.
// Returns the final exit code, or an error if the stream fails / start error frame.
func (c *Client) ExecStream(ctx context.Context, opts ExecOpts, onFrame func(ExecFrame) error) (exitCode int, err error) {
	if opts.Cmd == "" {
		return -1, fmt.Errorf("cmd is required")
	}
	if onFrame == nil {
		return -1, fmt.Errorf("onFrame is required")
	}
	u, err := url.Parse(c.base() + "/exec")
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
	res, err := c.longHTTP().Do(req)
	if err != nil {
		return -1, err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return -1, fmt.Errorf("exec stream: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}

	sc := bufio.NewScanner(res.Body)
	// Allow large stdout chunks in a single frame.
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
			return -1, fmt.Errorf("exec stream: %s", msg)
		}
	}
	if err := sc.Err(); err != nil {
		return -1, fmt.Errorf("exec stream read: %w", err)
	}
	if !gotExit {
		return -1, fmt.Errorf("exec stream: ended without exit frame")
	}
	return code, nil
}

// --- /cp -------------------------------------------------------------------

// PutFile uploads raw file bytes to guestPath via POST /cp?mode=binary.
// size is the Content-Length (-1 to omit / use chunked).
func (c *Client) PutFile(ctx context.Context, guestPath string, r io.Reader, size int64, opts CPOpts) error {
	if guestPath == "" {
		return fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/cp")
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

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	if size >= 0 {
		req.ContentLength = size
	}
	res, err := c.longHTTP().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("put file: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// GetFile downloads a file from guestPath via GET /cp?mode=binary into w.
func (c *Client) GetFile(ctx context.Context, guestPath string, w io.Writer) error {
	if guestPath == "" {
		return fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/cp")
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
	res, err := c.longHTTP().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("get file: not found")
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("get file: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(w, res.Body)
	return err
}

// PutTar extracts a tar stream at guestPath via POST /cp?mode=tar.
func (c *Client) PutTar(ctx context.Context, guestPath string, r io.Reader) error {
	if guestPath == "" {
		return fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/cp")
	if err != nil {
		return err
	}
	q := u.Query()
	q.Set("path", guestPath)
	q.Set("mode", "tar")
	u.RawQuery = q.Encode()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), r)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-tar")
	res, err := c.longHTTP().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("put tar: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	return nil
}

// GetTar downloads guestPath as a tar stream via GET /cp?mode=tar into w.
func (c *Client) GetTar(ctx context.Context, guestPath string, w io.Writer) error {
	if guestPath == "" {
		return fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/cp")
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
	res, err := c.longHTTP().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("get tar: not found")
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("get tar: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	_, err = io.Copy(w, res.Body)
	return err
}

// --- /fs -------------------------------------------------------------------

// ReadDir lists entries at guestPath via GET /fs/readdir.
func (c *Client) ReadDir(ctx context.Context, guestPath string) ([]FSInfo, error) {
	if guestPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/fs/readdir")
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
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("readdir: not found")
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("readdir: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var out []FSInfo
	if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("readdir decode: %w", err)
	}
	return out, nil
}

// Stat returns metadata for guestPath via GET /fs/stat.
func (c *Client) Stat(ctx context.Context, guestPath string) (*FSInfo, error) {
	if guestPath == "" {
		return nil, fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/fs/stat")
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
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("stat: not found")
	}
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("stat: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var info FSInfo
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("stat decode: %w", err)
	}
	return &info, nil
}

// Mkdir creates a directory via POST /fs/mkdir.
func (c *Client) Mkdir(ctx context.Context, guestPath string, recursive bool, mode string) error {
	if guestPath == "" {
		return fmt.Errorf("path is required")
	}
	body, err := json.Marshal(MkdirRequest{
		Path:      guestPath,
		Recursive: recursive,
		Mode:      mode,
	})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/fs/mkdir", strings.NewReader(string(body)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("mkdir: status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}

// Remove deletes guestPath via DELETE /fs/remove. If recursive, uses RemoveAll.
func (c *Client) Remove(ctx context.Context, guestPath string, recursive bool) error {
	if guestPath == "" {
		return fmt.Errorf("path is required")
	}
	u, err := url.Parse(c.base() + "/fs/remove")
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
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("remove: not found")
	}
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("remove: status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
