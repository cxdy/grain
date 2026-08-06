// Grain Desktop — Wails v2 operator console (thin client of the grain daemon API).
//
// Build (requires CGO + OS webview):
//
//	just desktop-build
package main

import (
	"embed"
	"flag"
	"log"
	"os"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/menu"
	"github.com/wailsapp/wails/v2/pkg/menu/keys"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	shellVM := flag.String("shell", "", "open shell-only window for this VM")
	flag.Parse()
	if v := os.Getenv("GRAIN_DESKTOP_SHELL"); v != "" && *shellVM == "" {
		*shellVM = v
	}

	app := NewApp()
	if *shellVM != "" {
		app.shellOnlyVM = *shellVM
	}

	title := "Grain"
	width, height := 1280, 840
	if *shellVM != "" {
		title = "Grain — " + *shellVM
		width, height = 900, 560
	}

	// App menu: Open (focus) + Quit — minimal tray substitute (window is primary).
	appMenu := menu.NewMenu()
	if *shellVM == "" {
		fileMenu := appMenu.AddSubmenu("Grain")
		fileMenu.AddText("Show window", keys.CmdOrCtrl("1"), func(_ *menu.CallbackData) {
			if app.ctx != nil {
				runtime.WindowShow(app.ctx)
				runtime.WindowUnminimise(app.ctx)
			}
		})
		fileMenu.AddSeparator()
		fileMenu.AddText("Quit", keys.CmdOrCtrl("q"), func(_ *menu.CallbackData) {
			if app.ctx != nil {
				runtime.Quit(app.ctx)
			} else {
				os.Exit(0)
			}
		})
	}

	err := wails.Run(&options.App{
		Title:     title,
		Width:     width,
		Height:    height,
		MinWidth:  640,
		MinHeight: 400,
		Menu:      appMenu,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 14, G: 16, B: 15, A: 255},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []interface{}{
			app,
		},
		Mac: &mac.Options{
			TitleBar:             mac.TitleBarDefault(),
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			About: &mac.AboutInfo{
				Title:   "Grain",
				Message: "Linux microVM sandboxes on your hardware.",
			},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
}
