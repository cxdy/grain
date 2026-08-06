//go:build !linux

package agent

import (
	"context"
	"log/slog"
)

func ensureClipboardX11(log *slog.Logger, fetch func(context.Context) ([]byte, error)) {
	_ = log
	_ = fetch
}
