package netutil

import (
	"context"
	"errors"
	"net"
	"os"
	"strings"
	"time"
)

// dialRetryAttempts is first try + retries on EHOSTUNREACH/ENETUNREACH.
const dialRetryAttempts = 4

// netDialContext is the TCP/unix dial hook (overridable in tests).
var netDialContext = defaultNetDialContext

func defaultNetDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	var d net.Dialer
	return d.DialContext(ctx, network, address)
}

// retrySleep is time.Sleep (overridable in tests).
var retrySleep = time.Sleep

// retryBackoff is the first delay between transient-dial retries.
var retryBackoff = 50 * time.Millisecond

// RetryDialContext dials like net.Dialer, but retries a few times when the
// kernel reports no route / network unreachable. macOS 15 (Sequoia) often
// fails the first LAN connect after sleep/unlock with EHOSTUNREACH even when
// Local Network permission is granted; a short retry usually succeeds.
func RetryDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return retryDialContext(ctx, netDialContext, network, address)
}

func retryDialContext(ctx context.Context, dial func(context.Context, string, string) (net.Conn, error), network, address string) (net.Conn, error) {
	var last error
	backoff := retryBackoff
	for attempt := 0; attempt < dialRetryAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			if last != nil {
				return nil, last
			}
			return nil, err
		}
		conn, err := dial(ctx, network, address)
		if err == nil {
			return conn, nil
		}
		last = err
		if !IsTransientDialError(err) {
			return nil, err
		}
		if attempt == dialRetryAttempts-1 {
			break
		}
		retrySleep(backoff)
		if backoff < 400*time.Millisecond {
			backoff *= 2
		}
	}
	return nil, last
}

// IsTransientDialError reports LAN routing failures that are often short-lived
// (macOS Local Network first-packet, Wi-Fi ARP, interface settle).
func IsTransientDialError(err error) bool {
	return IsHostUnreachable(err)
}

// IsHostUnreachable reports EHOSTUNREACH / ENETUNREACH (and the usual
// "no route to host" / "network is unreachable" strings from wrapped dials).
func IsHostUnreachable(err error) bool {
	if err == nil {
		return false
	}
	if errnoHostUnreachable(err) {
		return true
	}
	s := strings.ToLower(err.Error())
	return strings.Contains(s, "no route to host") || strings.Contains(s, "network is unreachable")
}

// IsDialTimeout reports a connect timeout or cancelled/deadline context.
func IsDialTimeout(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, os.ErrDeadlineExceeded) {
		return true
	}
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
