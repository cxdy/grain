package agent

import (
	"context"
	"fmt"
	"net/http"
	"time"
)

// WaitPollInterval is how often Wait polls the agent health endpoint.
const WaitPollInterval = 200 * time.Millisecond

// Wait polls HeadHealth every 200ms until the agent responds or ctx is done.
func Wait(ctx context.Context, c *Client) error {
	if c == nil {
		return fmt.Errorf("agent wait: nil client")
	}

	// Short-timeout client for polls so a hung connect does not burn the
	// full outer deadline on a single attempt.
	poll := *c
	if poll.HTTP == nil {
		poll.HTTP = &http.Client{Timeout: 500 * time.Millisecond}
	}

	// Immediate first attempt.
	if err := poll.HeadHealth(ctx); err == nil {
		return nil
	}

	ticker := time.NewTicker(WaitPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("wait for grain-agent: %w", ctx.Err())
		case <-ticker.C:
			if err := poll.HeadHealth(ctx); err == nil {
				return nil
			}
		}
	}
}
