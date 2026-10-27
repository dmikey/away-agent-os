package main

import (
	"embed"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"log"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	err = wails.Run(&options.App{Title: "Away OS", Width: 1440, Height: 950, MinWidth: 800, MinHeight: 600,
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: "ninja.therad.away", OnSecondInstanceLaunch: func(options.SecondInstanceData) { runtime.WindowShow(app.ctx); runtime.WindowUnminimise(app.ctx) }},
		BackgroundColour:   options.NewRGBA(232, 238, 225, 255), AssetServer: &assetserver.Options{Assets: assets},
		OnStartup: app.startup, OnShutdown: app.shutdown, Bind: []interface{}{app}})
	if err != nil {
		log.Fatal(err)
	}
}
