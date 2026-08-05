package agent

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"
	"github.com/cxdy/grain/internal/osc52"
	"golang.org/x/term"
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
	defer func() { _ = res.Body.Close() }()
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

// Readiness performs GET /readiness and decodes the Readiness body.
// When no readiness files exist, the guest returns an empty object (State "").
func (c *Client) Readiness(ctx context.Context) (*Readiness, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/readiness", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("readiness: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var r Readiness
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		return nil, fmt.Errorf("readiness decode: %w", err)
	}
	return &r, nil
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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

// --- /shell ----------------------------------------------------------------

// Shell opens an interactive PTY session over WebSocket (GET /shell).
// When Stdin is a local terminal and Raw is not explicitly false, the terminal
// is put into raw mode for the duration of the session. Window resize events
// (SIGWINCH) are forwarded as JSON control frames.
func (c *Client) Shell(ctx context.Context, opts ShellOpts) error {
	stdin := opts.Stdin
	if stdin == nil {
		stdin = os.Stdin
	}
	rawOut := opts.Stdout
	if rawOut == nil {
		rawOut = os.Stdout
	}
	stdout := rawOut
	// Host-side OSC 52 intercept so tools in the guest (Grok Build, etc.) can
	// set the *local* clipboard over grain sh / remote API shell. Disable with
	// GRAIN_OSC52_CLIPBOARD=0.
	if osc52.Enabled() {
		stdout = osc52.NewWriter(stdout)
	}
	// Stderr is reserved for future local diagnostics (raw-mode / dial hints).
	_ = opts.Stderr

	cols := opts.Cols
	rows := opts.Rows
	if cols <= 0 || rows <= 0 {
		if f, ok := stdin.(*os.File); ok {
			if w, h, err := term.GetSize(int(f.Fd())); err == nil && w > 0 && h > 0 {
				if cols <= 0 {
					cols = w
				}
				if rows <= 0 {
					rows = h
				}
			}
		}
	}
	if cols <= 0 {
		cols = 80
	}
	if rows <= 0 {
		rows = 24
	}

	// http → ws scheme
	base := c.base()
	u, err := url.Parse(base + "/shell")
	if err != nil {
		return err
	}
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	q := u.Query()
	q.Set("cols", fmt.Sprintf("%d", cols))
	q.Set("rows", fmt.Sprintf("%d", rows))
	if opts.Shell != "" {
		q.Set("shell", opts.Shell)
	}
	// Forward host terminal identity so guest TUIs negotiate keyboard correctly
	// (Kitty protocol / Shift+Enter, tmux extended keys, truecolor, …).
	env := opts.ExtraEnv
	if len(env) == 0 {
		env = HostShellExtraEnv()
	}
	shellEnvToQuery(q, env)
	u.RawQuery = q.Encode()

	// Pass HTTP client so vsock (custom Transport) and timeouts apply to WS upgrade.
	conn, _, err := websocket.Dial(ctx, u.String(), &websocket.DialOptions{
		HTTPClient: c.longHTTP(),
	})
	if err != nil {
		return fmt.Errorf("shell dial: %w", err)
	}
	defer func() { _ = conn.CloseNow() }()
	// Interactive shells can produce large bursts (e.g. cat of big files).
	conn.SetReadLimit(8 << 20)

	// Raw mode for local TTY. On exit always restore cooked mode and clear
	// private modes (alt screen, mouse, etc.) so a daemon restart mid-session
	// does not leave the remote terminal unusable.
	wantRaw := true
	if opts.Raw != nil {
		wantRaw = *opts.Raw
	}
	var restore func()
	if wantRaw {
		if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
			old, err := term.MakeRaw(int(f.Fd()))
			if err != nil {
				return fmt.Errorf("terminal raw mode: %w", err)
			}
			restore = func() { _ = term.Restore(int(f.Fd()), old) }
		}
	}
	defer func() {
		if restore != nil {
			restore()
		}
		// After cooked mode: clear modes left by guest TUIs / mid-stream disconnect.
		resetTerminalAfterShell(rawOut)
	}()

	// SIGWINCH → resize control frames.
	if f, ok := stdin.(*os.File); ok && term.IsTerminal(int(f.Fd())) {
		winch := make(chan os.Signal, 1)
		signal.Notify(winch, syscall.SIGWINCH)
		defer signal.Stop(winch)
		go func() {
			for {
				select {
				case <-ctx.Done():
					return
				case <-winch:
					w, h, err := term.GetSize(int(f.Fd()))
					if err != nil || w <= 0 || h <= 0 {
						continue
					}
					payload, _ := json.Marshal(ShellControl{Type: "resize", Cols: w, Rows: h})
					// Best-effort; session may already be closing.
					_ = conn.Write(ctx, websocket.MessageText, payload)
				}
			}
		}()
	}

	errCh := make(chan error, 2)

	// Local stdin → WS binary
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, readErr := stdin.Read(buf)
			if n > 0 {
				if werr := conn.Write(ctx, websocket.MessageBinary, buf[:n]); werr != nil {
					errCh <- werr
					return
				}
			}
			if readErr != nil {
				// EOF on stdin: stop sending; keep reading PTY until server closes.
				if readErr == io.EOF {
					return
				}
				errCh <- readErr
				return
			}
		}
	}()

	// WS → local stdout (server close ends the session).
	// Text frames may be control messages (clipboard_get) from the agent.
	go func() {
		for {
			typ, data, rerr := conn.Read(ctx)
			if rerr != nil {
				errCh <- rerr
				return
			}
			switch typ {
			case websocket.MessageBinary:
				if _, werr := stdout.Write(data); werr != nil {
					errCh <- werr
					return
				}
			case websocket.MessageText:
				var ctrl ShellControl
				if jerr := json.Unmarshal(data, &ctrl); jerr == nil && ctrl.Type == "clipboard_get" {
					reply := ShellControl{Type: "clipboard", Id: ctrl.Id}
					if clip, cerr := osc52.ReadClipboard(); cerr != nil {
						reply.Error = cerr.Error()
					} else {
						reply.Data = base64.StdEncoding.EncodeToString(clip)
					}
					payload, _ := json.Marshal(reply)
					if werr := conn.Write(ctx, websocket.MessageText, payload); werr != nil {
						errCh <- werr
						return
					}
					continue
				}
				// Unknown text: still surface to stdout (forward compatibility).
				if _, werr := stdout.Write(data); werr != nil {
					errCh <- werr
					return
				}
			}
		}
	}()

	// Wait for first fatal error or context cancel.
	// defer always restores cooked mode + private terminal modes.
	select {
	case <-ctx.Done():
		_ = conn.Close(websocket.StatusGoingAway, "canceled")
		return ctx.Err()
	case err := <-errCh:
		// Clean guest exit (normal WebSocket close / bare EOF).
		if isWSNormalClose(err) || err == io.EOF {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return nil
		}
		if err != nil {
			_ = conn.Close(websocket.StatusNormalClosure, "")
			// Daemon restart / network drop: clearer message + TTY reset via defer.
			return wrapShellSessionError(err)
		}
		return nil
	}
}

func isWSNormalClose(err error) bool {
	if err == nil {
		return true
	}
	// coder/websocket returns Status* errors via websocket.CloseError / CloseStatus.
	cs := websocket.CloseStatus(err)
	switch cs {
	case websocket.StatusNormalClosure, websocket.StatusGoingAway:
		return true
	}
	// Also treat generic closed / EOF as normal session end.
	msg := err.Error()
	if strings.Contains(msg, "status = StatusNormalClosure") ||
		strings.Contains(msg, "status = StatusGoingAway") ||
		strings.Contains(msg, "failed to get reader: received close frame") {
		return true
	}
	return false
}

// --- /stats ----------------------------------------------------------------

// Stats fetches guest resource basics via GET /stats.
func (c *Client) Stats(ctx context.Context) (*Stats, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.base()+"/stats", nil)
	if err != nil {
		return nil, err
	}
	res, err := c.http().Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("stats: status %d: %s", res.StatusCode, strings.TrimSpace(string(body)))
	}
	var st Stats
	if err := json.NewDecoder(res.Body).Decode(&st); err != nil {
		return nil, fmt.Errorf("stats decode: %w", err)
	}
	return &st, nil
}

// --- /secrets/materialize --------------------------------------------------

// MaterializeSecret writes a secret file into the guest via POST /secrets/materialize.
func (c *Client) MaterializeSecret(ctx context.Context, req MaterializeSecretRequest) (*MaterializeSecretResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.base()+"/secrets/materialize", strings.NewReader(string(body)))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http().Do(httpReq)
	if err != nil {
		return nil, err
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK && res.StatusCode != http.StatusNoContent {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return nil, fmt.Errorf("materialize secret: status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	var out MaterializeSecretResponse
	if res.StatusCode == http.StatusOK {
		if err := json.NewDecoder(res.Body).Decode(&out); err != nil {
			return nil, fmt.Errorf("materialize secret decode: %w", err)
		}
	}
	return &out, nil
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
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
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode == http.StatusNotFound {
		return fmt.Errorf("remove: not found")
	}
	if res.StatusCode != http.StatusNoContent && res.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(io.LimitReader(res.Body, 4<<10))
		return fmt.Errorf("remove: status %d: %s", res.StatusCode, strings.TrimSpace(string(b)))
	}
	return nil
}
