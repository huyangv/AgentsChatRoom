// Package bridge 实现 chatroom-bridge 命令：给 agent 用的 MCP 服务，以及 SessionStart / Stop hook。
// 两者都通过本机 HTTP 接口和聊天室应用通信。
package bridge

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"time"

	"agentchatroom/internal/room"
)

// DefaultURL 是聊天室应用内置服务的地址，可用环境变量 AGENT_ROOM_URL 覆盖。
const DefaultURL = "http://127.0.0.1:8765"

// Client 调用聊天室应用的本机接口。
type Client struct {
	BaseURL string
	HTTP    *http.Client
}

// NewClient 按环境变量 AGENT_ROOM_URL 创建客户端，未设置时用 DefaultURL。
func NewClient() *Client {
	base := os.Getenv("AGENT_ROOM_URL")
	if base == "" {
		base = DefaultURL
	}
	// 只连本机，不走系统代理
	return &Client{BaseURL: base, HTTP: &http.Client{Transport: &http.Transport{Proxy: nil}}}
}

// PostResult 是发言接口的返回。
type PostResult struct {
	Message room.Message `json:"message"`
	Woke    []string     `json:"woke"`
}

// WaitResult 是等待接口的返回。
type WaitResult struct {
	Wake     bool           `json:"wake"`
	Messages []room.Message `json:"messages"`
}

func (c *Client) Messages(ctx context.Context) ([]room.Message, error) {
	var out struct {
		Messages []room.Message `json:"messages"`
	}
	err := c.do(ctx, http.MethodGet, "/api/messages", nil, &out)
	return out.Messages, err
}

func (c *Client) Post(ctx context.Context, sender, text string) (PostResult, error) {
	var out PostResult
	err := c.do(ctx, http.MethodPost, "/api/messages", map[string]string{"sender": sender, "text": text}, &out)
	return out, err
}

func (c *Client) Register(ctx context.Context, agent string, s room.Session) error {
	body := map[string]string{"agent": agent, "sessionId": s.SessionID, "cwd": s.CWD, "socket": s.Socket}
	return c.do(ctx, http.MethodPost, "/api/register", body, nil)
}

func (c *Client) Wait(ctx context.Context, agent string, timeout time.Duration) (WaitResult, error) {
	var out WaitResult
	path := fmt.Sprintf("/api/wait?agent=%s&timeout=%d", url.QueryEscape(agent), int(timeout.Seconds()))
	err := c.do(ctx, http.MethodGet, path, nil, &out)
	return out, err
}

func (c *Client) do(ctx context.Context, method, path string, body, out any) error {
	var reader io.Reader
	if body != nil {
		data, err := json.Marshal(body)
		if err != nil {
			return err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.BaseURL+path, reader)
	if err != nil {
		return err
	}
	req.Header.Set(room.HeaderName, "1")
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Error string `json:"error"`
		}
		_ = json.Unmarshal(data, &e)
		return fmt.Errorf("聊天室返回 %d：%s", resp.StatusCode, e.Error)
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(data, out)
}
