package main

import (
	"embed"
	"log"

	"github.com/wailsapp/wails/v3/pkg/application"

	"agentchatroom/internal/room"
)

// 前端构建产物嵌入到二进制里。
//
//go:embed all:frontend/dist
var assets embed.FS

func init() {
	// 注册自定义事件及其数据类型，绑定生成器会据此生成前端类型。
	application.RegisterEvent[room.Message]("room:message")
	application.RegisterEvent[room.Status]("room:status")
}

func main() {
	app := application.New(application.Options{
		Name:        "AgentChatRoom",
		Description: "让多个 agent 和你一起讨论需求的聊天室",
		Services: []application.Service{
			application.NewService(&RoomService{}),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title:  "Agent 聊天室",
		Width:  1000,
		Height: 720,
		URL:    "/",
	})

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
