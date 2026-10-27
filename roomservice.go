package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v3/pkg/application"

	"agentchatroom/internal/room"
)

// roomAddr 是给 hook 和 MCP 用的本机接口地址，chatroom-bridge 默认连这里。
const roomAddr = "127.0.0.1:8765"

// RoomService 把聊天室暴露给前端，并在本机端口提供给 hook 和 MCP 调用的接口。
type RoomService struct {
	room    *room.Room
	server  *http.Server
	logFile *os.File
}

// ServiceStartup 创建聊天室并启动本机接口。端口被占用时应用启动失败。
func (s *RoomService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	var logWriter io.Writer
	if f, err := openLogFile(); err != nil {
		log.Printf("聊天记录文件打开失败，本次不记录：%v", err)
	} else {
		s.logFile = f
		logWriter = f
	}

	s.room = room.New([]string{"claude", "codex"}, 6, logWriter)
	app := application.Get()
	s.room.OnMessage = func(m room.Message) { app.Event.Emit("room:message", m) }
	s.room.OnStatus = func(st room.Status) { app.Event.Emit("room:status", st) }

	ln, err := net.Listen("tcp", roomAddr)
	if err != nil {
		return fmt.Errorf("聊天室端口 %s 不可用：%w", roomAddr, err)
	}
	s.server = &http.Server{Handler: room.NewHandler(s.room, ln.Addr().String())}
	go func() { _ = s.server.Serve(ln) }()
	return nil
}

// ServiceShutdown 关闭本机接口。正在等待消息的 hook 会立刻收到连接断开，agent 随之正常停下。
func (s *RoomService) ServiceShutdown() error {
	if s.server != nil {
		_ = s.server.Close()
	}
	if s.logFile != nil {
		_ = s.logFile.Close()
	}
	return nil
}

// Send 以用户身份发言。
func (s *RoomService) Send(text string) (room.Message, error) {
	msg, _, err := s.room.Post(room.User, text)
	return msg, err
}

// History 返回 ID 大于 after 的消息，最多最近 200 条。
func (s *RoomService) History(after int) []room.Message {
	return s.room.History(after)
}

// Status 返回各 agent 的在线状态。
func (s *RoomService) Status() room.Status {
	return s.room.Status()
}

// openLogFile 打开聊天记录文件：用户配置目录下的 AgentChatRoom/room.jsonl（macOS 为 ~/Library/Application Support）。
func openLogFile() (*os.File, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return nil, err
	}
	dir = filepath.Join(dir, "AgentChatRoom")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "room.jsonl"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
}
