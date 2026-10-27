# 交接说明（2026-10-27）

给接手这个项目的新会话看。先读本文件，再读 README.md。

## 目标

让 Claude Code 桌面版和 Codex 桌面版能和用户在同一个聊天室里讨论需求：用户提问，agent 互相讨论，中途可以问用户，最后由用户确认方案。第一阶段只支持这两个桌面版，不走 CLI 交互。

技术栈固定为 Go + Wails v3.0.0 + Vue 3 + Naive UI，不要用 Python。

## 当前方案（技术验证版）

分两层：

- **说话层**：MCP。`chatroom-bridge mcp` 提供 `chat_read`、`chat_send` 两个工具，两家桌面版通用。
- **叫醒层**：Stop hook 长等待。agent 每轮结束时，`chatroom-bridge hook stop` 去聊天室等消息；等到了就返回 `{"decision":"block","reason":<新消息>}`，agent 把它当下一条提示继续工作。等 1740 秒没有消息就放行，hook 超时设的是 1800 秒。

聊天室应用在 `127.0.0.1:8765` 提供本机接口，要求请求带 `X-Agent-Room: 1` 头且 Host 是本机地址，用来挡住外部网页。

## 选这个方案的依据

- MCP 只能由 agent 主动调用，服务端没法叫醒空闲的 agent，所以必须另有叫醒机制。
- Claude Code 的 channels 功能只能在命令行启动时开启，桌面版没有入口（2026 年 5 月的功能请求还开着），所以不用。
- Codex 的 Stop hook 返回 `decision: block` 时，会把 `reason` 当作新的用户提示继续工作；hook 默认超时 600 秒，可以配置。
- Claude Code 的 Stop hook 默认最多连续阻止停止 8 次。已在 `playground/.claude/settings.json` 里把 `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP` 设为 `0` 关掉这个上限，是否生效还没验证。
- 备选叫醒方式（暂未采用）：
  - Claude 这边可用跨会话消息的收件箱 socket（v2.1.224+）。会话空闲时收到消息会自动开新一轮，桌面版也支持；但除第一行认证外，报文格式文档没写。SessionStart hook 已经把 `CLAUDE_CODE_MESSAGING_SOCKET` 登记到聊天室，留作后用。
  - Codex 这边可用 app-server 的 `thread/resume` + `turn/start`，但外部发起的新一轮能不能在桌面窗口里同步显示，还要验证。

## 代码结构

| 路径 | 内容 |
|---|---|
| `internal/room/` | 聊天室核心（消息、按 @ 唤醒、轮次上限）和本机 HTTP 接口，只用标准库，有测试 |
| `internal/bridge/` | MCP 服务（手写的 stdio JSON-RPC，没有引入 MCP SDK）和 hook 逻辑，有测试 |
| `cmd/chatroom-bridge/` | 命令入口：`mcp` / `hook session-start` / `hook stop` |
| `main.go`、`roomservice.go` | Wails 应用和暴露给前端的 `RoomService`（`Send` / `History` / `Status`），通过事件 `room:message`、`room:status` 推送更新 |
| `frontend/src/` | `App.vue`（Naive UI 主题跟随系统）、`components/ChatRoom.vue` |
| `playground/` | 讨论用目录，含两家的 MCP、hook 配置和 `AGENTS.md` 聊天室规则 |
| `scripts/setup.sh` | 用 `wails3 init -t vue` 补齐骨架，安装 naive-ui，构建 bridge，替换配置里的 `__ROOT__` |

注意：开发本项目的会话要开在项目根目录，**不要开在 `playground`**，否则 Stop hook 会让会话一直等聊天室消息。

## 验证情况

已验证（在 Linux 上）：
- `go test -race ./internal/...` 全部通过。
- 用 bridge 二进制端到端跑通：用户 @claude → claude 被叫醒 → claude 通过 MCP 发言 @codex → codex 被叫醒。
- 聊天室没开时 hook 立即放行。

已验证（在 macOS 上，2026-10-27）：
- `scripts/setup.sh` 跑通。修过一处：macOS 的 `cp -n` 跳过已有文件时返回非零，脚本会中途退出，已改成逐个补缺失文件。
- `vue-tsc` 类型检查、前端构建、Wails 主程序编译都通过；`wails3 dev` 能启动窗口，本机接口 8765 正常（不带请求头或 Host 不对时返回 403）。

未验证：
- 桌面版里的实际表现，即 README 里的 7 项验证表，还没跑。

## 下一步

1. 按 README 的验证表在两个桌面版里实测，记录哪些项通过。
2. 根据结果决定叫醒方式是否换成上面的备选方案。
3. 第二阶段功能：
   - `ask_user` 工具：暂停讨论，在界面上弹出问题卡片。
   - `propose_plan` 工具：生成方案，由用户在界面上确认或驳回；确认后写入目标仓库。
   - 讨论真实项目，而不只是 `playground`。
   - 每种 agent 支持多个会话。
