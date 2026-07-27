// Package tray provides a menu bar / system tray for grain (macOS + Linux).
package tray

import (
	"context"
	_ "embed"
	"os"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/systray"
)

//go:embed icon.png
var iconPNG []byte

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

// Run blocks until the tray exits.
func Run(opts Options) error {
	if opts.Status == nil {
		opts.Status = func(context.Context) Status {
			return Status{Title: "grain", Tooltip: "grain"}
		}
	}
	systray.Run(func() { onReady(opts) }, func() {
		if opts.OnQuit != nil {
			opts.OnQuit()
		}
	})
	return nil
}

func onReady(opts Options) {
	systray.SetTemplateIcon(iconPNG, iconPNG)
	systray.SetTitle("grain")
	systray.SetTooltip("grain microVMs")

	mStatus := systray.AddMenuItem("Daemon…", "Current status")
	mStatus.Disable()
	systray.AddSeparator()
	mDocs := systray.AddMenuItem("Open docs", "grainvm.com")
	mQuick := systray.AddMenuItem("Quick start", "Quick start guide")
	systray.AddSeparator()
	mQuit := systray.AddMenuItem("Quit tray", "Leave the tray (daemon keeps running)")

	// Status poller
	go func() {
		t := time.NewTicker(3 * time.Second)
		defer t.Stop()
		update := func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			st := opts.Status(ctx)
			cancel()
			if st.Title != "" {
				systray.SetTitle(st.Title)
			}
			if st.Tooltip != "" {
				systray.SetTooltip(st.Tooltip)
				mStatus.SetTitle(st.Tooltip)
			}
		}
		update()
		for range t.C {
			update()
		}
	}()

	go func() {
		for {
			select {
			case <-mDocs.ClickedCh:
				openBrowser("https://grainvm.com")
			case <-mQuick.ClickedCh:
				openBrowser("https://grainvm.com/get-started/quickstart/")
			case <-mQuit.ClickedCh:
				systray.Quit()
				return
			}
		}
	}()
	_ = opts.Version
}

func openBrowser(u string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", u)
	case "linux":
		cmd = exec.Command("xdg-open", u)
	default:
		return
	}
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	_ = cmd.Start()
}
