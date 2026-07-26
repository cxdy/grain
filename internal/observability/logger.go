package observability

import (
	"log/slog"
	"os"
	"strings"
)

// NewLogger returns a structured JSON logger (good for shipping to Loki/otel later).
// Level: debug|info|warn|error
func NewLogger(level string) *slog.Logger {
	var lv slog.Level
	switch strings.ToLower(level) {
	case "debug":
		lv = slog.LevelDebug
	case "warn":
		lv = slog.LevelWarn
	case "error":
		lv = slog.LevelError
	default:
		lv = slog.LevelInfo
	}
	h := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: lv})
	return slog.New(h).With("service", "grain")
}
