package cli

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/cxdy/grain/internal/agent"
	"github.com/cxdy/grain/internal/api"
)

// errAgentSkip means the agent path was not usable (no port / unhealthy); fall back to SSH.
var errAgentSkip = fmt.Errorf("agent skip")

// dialGuestAgent builds an agent.Client via agent.Dial (vsock when AgentCID > 0,
// else TCP hostfwd). When force is false and the agent is unavailable, returns errAgentSkip.
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
	target := agent.Target{CID: inst.AgentCID, Port: inst.AgentPort}
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

// execViaAgent dials the guest agent and runs remote via ExecStream (live stdout/stderr).
// Falls back to buffered exec if streaming fails before the process starts.
// On success prints output and returns nil or exitCodeError.
// force requires agent; otherwise returns errAgentSkip when unavailable.
func execViaAgent(c *api.Client, name string, remote []string, force bool) error {
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
			fmt.Fprint(os.Stderr, f.Data)
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
		fmt.Fprint(os.Stderr, res.Stderr)
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
