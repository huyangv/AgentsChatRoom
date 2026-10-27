package bridge

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

var supportedVersions = map[string]bool{"2025-11-25": true, "2025-06-18": true, "2025-03-26": true, "2024-11-05": true}

const fallbackVersion = "2025-06-18"

var tools = []map[string]any{
	{
		"name":        "chat_read",
		"description": "读取聊天室最近的消息，包括用户和其他 agent 的发言。",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"limit": map[string]any{"type": "integer", "description": "最多返回多少条，默认 30"}},
		},
	},
	{
		"name": "chat_send",
		"description": "以你的身份在聊天室发言。需要另一个 agent 回应时，在内容里 @ 它（@claude 或 @codex）；" +
			"需要用户决定时直接向用户提问，不要 @ 任何 agent。",
		"inputSchema": map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string", "description": "发言内容"}},
			"required":   []string{"text"},
		},
	},
}

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type rpcResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
}

// ServeMCP 在 stdio 上提供 MCP 服务（每行一条 JSON-RPC 消息），直到输入结束。
// agent 是发言时使用的身份。
func ServeMCP(ctx context.Context, c *Client, agent string, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	enc := json.NewEncoder(out)
	enc.SetEscapeHTML(false)
	for {
		line, err := reader.ReadBytes('\n')
		if len(strings.TrimSpace(string(line))) > 0 {
			if resp := handleLine(ctx, c, agent, line); resp != nil {
				if werr := enc.Encode(resp); werr != nil {
					return werr
				}
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

func handleLine(ctx context.Context, c *Client, agent string, line []byte) *rpcResponse {
	var req rpcRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return &rpcResponse{JSONRPC: "2.0", ID: json.RawMessage("null"), Error: &rpcError{Code: -32700, Message: "Parse error"}}
	}
	if len(req.ID) == 0 || string(req.ID) == "null" {
		return nil // 通知类消息不回复
	}
	ok := func(result any) *rpcResponse { return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Result: result} }

	switch req.Method {
	case "initialize":
		var p struct {
			ProtocolVersion string `json:"protocolVersion"`
		}
		_ = json.Unmarshal(req.Params, &p)
		version := fallbackVersion
		if supportedVersions[p.ProtocolVersion] {
			version = p.ProtocolVersion
		}
		return ok(map[string]any{
			"protocolVersion": version,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "agent-chatroom", "version": "0.1.0"},
		})
	case "ping":
		return ok(map[string]any{})
	case "tools/list":
		return ok(map[string]any{"tools": tools})
	case "tools/call":
		var p struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		}
		_ = json.Unmarshal(req.Params, &p)
		text, err := callTool(ctx, c, agent, p.Name, p.Arguments)
		if err != nil {
			return ok(map[string]any{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true})
		}
		return ok(map[string]any{"content": []map[string]string{{"type": "text", "text": text}}})
	default:
		return &rpcResponse{JSONRPC: "2.0", ID: req.ID, Error: &rpcError{Code: -32601, Message: "Method not found: " + req.Method}}
	}
}

func callTool(ctx context.Context, c *Client, agent, name string, args map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	switch name {
	case "chat_read":
		limit := 30
		if v, ok := args["limit"].(float64); ok && v > 0 {
			limit = int(v)
		}
		msgs, err := c.Messages(ctx)
		if err != nil {
			return "", fmt.Errorf("连不上聊天室（%s）：%w", c.BaseURL, err)
		}
		if len(msgs) == 0 {
			return "聊天室还没有消息。", nil
		}
		if len(msgs) > limit {
			msgs = msgs[len(msgs)-limit:]
		}
		lines := make([]string, 0, len(msgs))
		for _, m := range msgs {
			lines = append(lines, fmt.Sprintf("[#%d %s] %s", m.ID, m.Sender, m.Text))
		}
		return strings.Join(lines, "\n"), nil
	case "chat_send":
		text, _ := args["text"].(string)
		if strings.TrimSpace(text) == "" {
			return "", errors.New("text 不能为空")
		}
		res, err := c.Post(ctx, agent, text)
		if err != nil {
			return "", fmt.Errorf("发送失败（%s）：%w", c.BaseURL, err)
		}
		if len(res.Woke) == 0 {
			return fmt.Sprintf("已发送（#%d），没有通知其他 agent。", res.Message.ID), nil
		}
		return fmt.Sprintf("已发送（#%d），已通知：%s。", res.Message.ID, strings.Join(res.Woke, "、")), nil
	default:
		return "", fmt.Errorf("未知工具：%s", name)
	}
}
