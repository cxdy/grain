package cli

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
)

// errAgentSkip means the agent path was not usable (no port / unhealthy); fall back to SSH.
var errAgentSkip = fmt.Errorf("agent skip")

// dialGuestAgent builds an agent.Client via agent.Dial (Firecracker UDS,
// AF_VSOCK, or TCP hostfwd). When force is false and the agent is unavailable,
// returns errAgentSkip.
//
// When remote is true, the CLI cannot reach hostfwd ports or host UDS on the
// daemon host; callers must use daemon-proxied API paths instead
// (see execViaDaemonAPI, etc.).
func dialGuestAgent(c *api.Client, name string, force bool) (*agent.Client, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	inst, err := c.Get(ctx, name)
	if err != nil {
		if force {
			return nil, err
		}
		return nil, errAgentSkip
	}
	target := agent.TargetForInstance(inst.AgentCID, inst.AgentPort, inst.DiskPath)
	if !target.HasEndpoint() {
		if force {
			return nil, fmt.Errorf("agent not available (no agent endpoint for %s)", name)
		}
		return nil, errAgentSkip
	}

	ac, err := agent.Dial(ctx, target)
	if err != nil {
		if force {
			return nil, fmt.Errorf("agent not available for %s: %w", name, err)
		}
		return nil, errAgentSkip
	}

	hctx, hcancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer hcancel()
	if err := ac.HeadHealth(hctx); err != nil {
		if force {
			return nil, fmt.Errorf("agent not available for %s: %w", name, err)
		}
		return nil, errAgentSkip
	}
	return ac, nil
}

// shellViaDaemon opens an interactive PTY through the daemon WebSocket proxy
// (GET /vms/{name}/shell). Used for remote CLI mode.
func shellViaDaemon(c *api.Client, name string) error {
	// agent.Client.Shell dials BaseURL+"/shell"; point BaseURL at the daemon VM route.
	ac := &agent.Client{
		BaseURL: strings.TrimRight(c.Base, "/") + "/vms/" + url.PathEscape(name),
		HTTP:    daemonHTTP(c),
	}
	_, _ = fmt.Fprintf(os.Stderr, "connecting via agent (remote API) to %s …\n", name)
	// Explicit ExtraEnv so remote sessions forward the *client* terminal identity
	// (TERM_PROGRAM / LC_TERMINAL / TERM_FEATURES / …) for Shift+Enter and friends.
	return ac.Shell(context.Background(), agent.ShellOpts{
		ExtraEnv: agent.HostShellExtraEnv(),
	})
}

// daemonHTTP returns an *http.Client that sends the same Bearer token as the API client.
func daemonHTTP(c *api.Client) *http.Client {
	// Re-use the API client's transport/timeouts via a thin RoundTripper that
	// injects Authorization when Token is set (mirrors api.Client.http()).
	base := c.HTTP
	if base == nil {
		base = &http.Client{}
	}
	if c.Token == "" {
		return base
	}
	clone := *base
	rt := base.Transport
	if rt == nil {
		rt = http.DefaultTransport
	}
	clone.Transport = &bearerRT{base: rt, token: c.Token}
	return &clone
}

type bearerRT struct {
	base  http.RoundTripper
	token string
}

func (b *bearerRT) RoundTrip(req *http.Request) (*http.Response, error) {
	req2 := req.Clone(req.Context())
	req2.Header.Set("Authorization", "Bearer "+b.token)
	return b.base.RoundTrip(req2)
}

// execViaAgent dials the guest agent and runs remote via ExecStream (live stdout/stderr).
// Falls back to buffered exec if streaming fails before the process starts.
// On success prints output and returns nil or exitCodeError.
// force requires agent; otherwise returns errAgentSkip when unavailable.
//
// When viaDaemon is true (remote CLI), uses the daemon's proxied /exec instead of
// dialing the hostfwd agent port.
func execViaAgent(c *api.Client, name string, remote []string, force bool, viaDaemon bool) error {
	if viaDaemon {
		return execViaDaemonAPI(c, name, remote)
	}

	ac, err := dialGuestAgent(c, name, force)
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	opts := agent.ExecOpts{Cmd: remote[0], Args: remote[1:]}

	started := false
	code, streamErr := ac.ExecStream(ctx, opts, func(f agent.ExecFrame) error {
		switch f.Type {
		case "started":
			started = true
		case "stdout":
			fmt.Print(f.Data)
		case "stderr":
			_, _ = fmt.Fprint(os.Stderr, f.Data)
		}
		return nil
	})
	if streamErr == nil {
		if code != 0 {
			return exitCodeError(code)
		}
		return nil
	}
	// Do not re-run if the process already started (partial stream already printed).
	if started {
		if force {
			return fmt.Errorf("agent exec stream: %w", streamErr)
		}
		return streamErr
	}

	// Stream setup/transport failed — buffered fallback.
	res, err := ac.ExecBufferedOpts(ctx, opts)
	if err != nil {
		if force {
			return fmt.Errorf("agent exec: %w", err)
		}
		return err
	}
	if res.Stdout != "" {
		fmt.Print(res.Stdout)
	}
	if res.Stderr != "" {
		_, _ = fmt.Fprint(os.Stderr, res.Stderr)
	}
	if res.Error != "" && res.ExitCode != 0 {
		if res.Stdout == "" && res.Stderr == "" {
			fmt.Fprintln(os.Stderr, res.Error)
		}
	}
	if res.ExitCode != 0 {
		return exitCodeError(res.ExitCode)
	}
	return nil
}

func execViaDaemonAPI(c *api.Client, name string, remote []string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	opts := agent.ExecOpts{Cmd: remote[0], Args: remote[1:]}
	code, err := c.ExecStream(ctx, name, opts, func(f agent.ExecFrame) error {
		switch f.Type {
		case "stdout":
			fmt.Print(f.Data)
		case "stderr":
			_, _ = fmt.Fprint(os.Stderr, f.Data)
		}
		return nil
	})
	if err != nil {
		// Buffered fallback through daemon.
		res, err2 := c.Exec(ctx, name, remote[0], remote[1:]...)
		if err2 != nil {
			return err
		}
		if res.Stdout != "" {
			fmt.Print(res.Stdout)
		}
		if res.Stderr != "" {
			_, _ = fmt.Fprint(os.Stderr, res.Stderr)
		}
		if res.ExitCode != 0 {
			return exitCodeError(res.ExitCode)
		}
		return nil
	}
	if code != 0 {
		return exitCodeError(code)
	}
	return nil
}
