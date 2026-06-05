package main

import (
	"embed"
	"fmt"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

//go:embed all:frontend/dist
var assets embed.FS

var (
	version = "0.1.0"
)

func main() {
	app := NewApp()

	err := wails.Run(&options.App{
		Title:  appTitle(),
		Width:  1180,
		Height: 760,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 34, G: 34, B: 38, A: 1},
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind: []any{
			app,
		},
	})
	if err != nil {
		println("error:", err.Error())
	}
}

func appTitle() string {
	return fmt.Sprintf("hikari v%s", strings.TrimPrefix(version, "v"))
}
