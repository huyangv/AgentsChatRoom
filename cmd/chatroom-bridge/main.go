// chatroom-bridge 是 agent 和聊天室应用之间的桥：
//
//	chatroom-bridge mcp --agent claude                  作为 MCP 服务运行（stdio）
//	chatroom-bridge hook session-start --agent claude   SessionStart hook：登记会话
//	chatroom-bridge hook stop --agent codex             Stop hook：等待聊天室新消息
//
// 聊天室地址默认 http://127.0.0.1:8765，可用环境变量 AGENT_ROOM_URL 覆盖。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"agentchatroom/internal/bridge"
)

const usage = `用法：
  chatroom-bridge mcp --agent <claude|codex>
  chatroom-bridge hook <session-start|stop> --agent <claude|codex> [--wait 秒数]`

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return fmt.Errorf("%s", usage)
	}
	ctx := context.Background()
	client := bridge.NewClient()

	switch args[0] {
	case "mcp":
		fs := flag.NewFlagSet("mcp", flag.ContinueOnError)
		agent := fs.String("agent", "", "发言身份：claude 或 codex")
		if err := fs.Parse(args[1:]); err != nil {
			return err
		}
		if err := checkAgent(*agent); err != nil {
			return err
		}
		return bridge.ServeMCP(ctx, client, *agent, os.Stdin, os.Stdout)
	case "hook":
		if len(args) < 2 {
			return fmt.Errorf("%s", usage)
		}
		event := args[1]
		fs := flag.NewFlagSet("hook", flag.ContinueOnError)
		agent := fs.String("agent", "", "agent 身份：claude 或 codex")
		wait := fs.Int("wait", 1740, "Stop 时最多等待多少秒，需小于 hook 配置的超时")
		if err := fs.Parse(args[2:]); err != nil {
			return err
		}
		if err := checkAgent(*agent); err != nil {
			return err
		}
		return bridge.RunHook(ctx, client, event, *agent, time.Duration(*wait)*time.Second, os.Stdin, os.Stdout)
	default:
		return fmt.Errorf("%s", usage)
	}
}

func checkAgent(agent string) error {
	if agent != "claude" && agent != "codex" {
		return fmt.Errorf("--agent 必须是 claude 或 codex")
	}
	return nil
}
