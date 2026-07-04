// [INPUT]: 依赖 wails/v2 runtime；internal/webstudio 的 Handler + 本包 App
// [OUTPUT]: AskDAO Studio 桌面 app 入口（Wails main）
// [POS]: cmd 第二入口 —— 桌面壳，AssetServer.Handler 复用 webstudio 的 http 表面（零 sidecar、零前端构建链）
// [PROTOCOL]: 变更时更新此头部，然后检查 CLAUDE.md
package main

import (
	"log"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"github.com/askdao/askdao-cli/internal/webstudio"
)

func main() {
	app := NewApp()

	// Desktop reuses the existing webstudio HTTP surface as the Wails AssetServer
	// handler. With Assets:nil every GET is routed to it, so studio.html + /api/* +
	// /logo.png are served with no sidecar server and no embedded frontend bundle —
	// frontend:install/build stay empty in wails.json, so `wails build` touches no
	// frontend and only needs this handler.
	studioHandler := webstudio.Handler(app.StudioOptions())

	if err := wails.Run(&options.App{
		Title:     "AskDAO Studio",
		Width:     1200,
		Height:    800,
		MinWidth:  900,
		MinHeight: 640,
		AssetServer: &assetserver.Options{
			Handler: studioHandler,
		},
		OnStartup: app.startup,
		Bind:      []interface{}{app},
	}); err != nil {
		log.Fatal(err)
	}
}
