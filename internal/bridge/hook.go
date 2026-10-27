package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"agentchatroom/internal/room"
)

// hookEvent 是 Claude Code 和 Codex 通过标准输入传给 hook 的 JSON 中本工具用到的字段。
type hookEvent struct {
	SessionID string `json:"session_id"`
	CWD       string `json:"cwd"`
}

// RunHook 处理一次 hook 调用。
//
//   - session-start：向聊天室登记会话，不向标准输出写任何内容（SessionStart 的输出会进入模型上下文）。
//   - stop：等待聊天室里发给该 agent 的新消息。等到了就输出 {"decision":"block","reason":...}，
//     让 agent 把新消息当作下一条提示继续工作；没等到（超时或聊天室没开）就放行，让 agent 正常停下。
func RunHook(ctx context.Context, c *Client, event, agent string, wait time.Duration, stdin io.Reader, stdout io.Writer) error {
	var ev hookEvent
	if data, err := io.ReadAll(stdin); err == nil && len(strings.TrimSpace(string(data))) > 0 {
		_ = json.Unmarshal(data, &ev)
	}

	switch event {
	case "session-start":
		regCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		session := room.Session{SessionID: ev.SessionID, CWD: ev.CWD, Socket: os.Getenv("CLAUDE_CODE_MESSAGING_SOCKET")}
		_ = c.Register(regCtx, agent, session)
		return nil
	case "stop":
		waitCtx, cancel := context.WithTimeout(ctx, wait+15*time.Second)
		defer cancel()
		result, err := c.Wait(waitCtx, agent, wait)
		if err == nil && result.Wake && len(result.Messages) > 0 {
			return writeHookJSON(stdout, map[string]string{"decision": "block", "reason": formatReason(result.Messages)})
		}
		if agent == "codex" {
			// Codex 的 Stop hook 在退出码为 0 时要求标准输出是 JSON
			return writeHookJSON(stdout, map[string]string{})
		}
		return nil
	default:
		return fmt.Errorf("未知 hook 事件：%s", event)
	}
}

func formatReason(msgs []room.Message) string {
	var b strings.Builder
	b.WriteString("聊天室有新消息：\n")
	for _, m := range msgs {
		fmt.Fprintf(&b, "[%s] %s\n", m.Sender, m.Text)
	}
	b.WriteString("\n请用 chatroom 的 chat_send 工具回复，不要只在本窗口回答。" +
		"需要另一个 agent 回应时 @ 它；需要用户决定时直接问用户，不要 @ 任何 agent。")
	return b.String()
}

func writeHookJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
