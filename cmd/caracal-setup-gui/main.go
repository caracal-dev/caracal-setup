package main

import (
	"log"

	"github.com/caracal-os/caracal-setup/internal/guiapp"
	"github.com/caracal-os/caracal-setup/internal/guiassets"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

func main() {
	frontend, err := guiassets.FrontendFS()
	if err != nil {
		log.Fatal(err)
	}

	app := guiapp.New()
	if err := wails.Run(&options.App{
		Title:            "Caracal Setup",
		Width:            1320,
		Height:           900,
		MinWidth:         1080,
		MinHeight:        720,
		BackgroundColour: options.NewRGBA(24, 22, 22, 255),
		AssetServer: &assetserver.Options{
			Assets: frontend,
		},
		OnStartup: app.Startup,
		Bind: []interface{}{
			app,
		},
	}); err != nil {
		log.Fatal(err)
	}
}
