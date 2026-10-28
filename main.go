package main

import (
	"embed"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"log"
	"os"
	"strconv"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	app, err := newApp()
	if err != nil {
		log.Fatal(err)
	}
	instanceID := "ninja.therad.away"
	if os.Getenv("AWAY_SMOKE_TEST") == "1" || os.Getenv("AWAY_VM_SMOKE_TEST") == "1" || os.Getenv("AWAY_TERMINAL_SMOKE_TEST") == "1" || os.Getenv("AWAY_SHARES_SMOKE_TEST") == "1" {
		instanceID += ".smoke." + strconv.Itoa(os.Getpid())
	}
	err = wails.Run(&options.App{Title: "Away OS", Width: 1440, Height: 950, MinWidth: 800, MinHeight: 600,
		SingleInstanceLock: &options.SingleInstanceLock{UniqueId: instanceID, OnSecondInstanceLaunch: func(options.SecondInstanceData) { runtime.WindowShow(app.ctx); runtime.WindowUnminimise(app.ctx) }},
		BackgroundColour:   options.NewRGBA(232, 238, 225, 255), AssetServer: &assetserver.Options{Assets: assets},
		OnStartup: app.startup, OnShutdown: app.shutdown, Bind: []interface{}{app}})
	if err != nil {
		log.Fatal(err)
	}
}
