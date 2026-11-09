// devt-tunnel: define, find and run tunnels from one place.
package main

import (
	"embed"
	"io"
	"io/fs"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"github.com/trieu04/devt-tunnel/internal/host"
	"github.com/trieu04/devt-tunnel/internal/platform"
)

var version = "0.1.0"

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	host.Version = version
	if stateDir, err := platform.StateDir(); err == nil {
		if file, err := platform.OpenRotatingLog(stateDir); err == nil {
			defer file.Close()
			log.SetOutput(io.MultiWriter(log.Writer(), file))
		}
	}
	log.Printf("starting devt-tunnel %s", version)
	dist, err := fs.Sub(assets, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	svc, err := host.NewService()
	if err != nil {
		log.Fatal(err)
	}
	app := application.New(application.Options{
		Name:        "devt-tunnel",
		Description: "Run SSH, cloudflared, ngrok and custom-command tunnels from one place",
		Services:    []application.Service{application.NewService(svc)},
		Assets:      application.AssetOptions{Handler: application.AssetFileServerFS(dist)},
		Mac:         application.MacOptions{ApplicationShouldTerminateAfterLastWindowClosed: false},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID:               "io.github.trieu04.devt-tunnel",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) { svc.ShowMain() },
		},
		OnShutdown: svc.Shutdown,
	})
	host.Attach(app, svc)
	log.Printf("running application loop")
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
