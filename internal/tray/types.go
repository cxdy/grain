// Package tray provides a menu bar / system tray for grain.
package tray

import "context"

// Status is a periodic snapshot for the tray title/tooltip.
type Status struct {
	Title   string
	Tooltip string
}

// Options configures the tray loop.
type Options struct {
	Version string
	// Status is polled every few seconds while the tray runs.
	Status func(ctx context.Context) Status
	OnQuit func()
}
