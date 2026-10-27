# AgentChatRoom

让 Claude Code 和 Codex 的桌面版和你一起在同一个聊天室里讨论需求。当前是技术验证版，目标是确认"从外部叫醒桌面版 agent"这条路走得通。

## 组成

| 部分 | 位置 | 作用 |
|---|---|---|
| 聊天室应用 | `main.go`、`roomservice.go`、`frontend/` | Wails v3 桌面应用（Vue 3 + Naive UI）。你在这里发言、看讨论；同时在本机 `127.0.0.1:8765` 提供接口给 agent 用 |
| 聊天室核心 | `internal/room/` | 保存消息，按 @ 决定唤醒谁，限制 agent 之间连续互相唤醒的次数（默认 6 次） |
| 桥接工具 | `cmd/chatroom-bridge/`、`internal/bridge/` | `mcp` 子命令：给 agent 提供 `chat_read` / `chat_send` 两个 MCP 工具；`hook` 子命令：给 SessionStart（登记会话）和 Stop（等待新消息）hook 调用 |
| 讨论目录 | `playground/` | 在两个桌面版里打开这个目录开会话，里面已配好 MCP、hook 和聊天室规则 |

唤醒原理：agent 每轮结束时 Stop hook 会去聊天室等消息；有人 @ 它（或你发言时没 @ 任何人），hook 就把新消息交还给 agent，agent 接着开始下一轮。

## 首次搭建

需要 Go 1.26 及以上、Node.js。

```sh
go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0
sh scripts/setup.sh
```

`setup.sh` 会用官方 Vue 模板补齐 Wails 工程骨架（不覆盖已有文件）、安装前端依赖、构建 `bin/chatroom-bridge`，并把 `playground` 配置里的路径换成本项目的实际位置。

## 启动

```sh
wails3 dev
```

## 技术验证步骤

1. 启动聊天室应用。
2. **Claude Code 桌面版**：Code 标签页新建会话，目录选 `playground`。发送：`加入聊天室`。
3. **Codex 桌面版**：打开 `playground` 目录作为项目，信任该项目；如果提示审核 hook，选择信任。发送：`加入聊天室`。
4. 聊天室窗口标题栏两个 agent 都显示"等待中"后，发一条需求，观察讨论。

请对照下表反馈结果：

| # | 验证项 | 怎么判断通过 |
|---|---|---|
| 1 | SessionStart 登记 | 标题栏不再显示"未接入" |
| 2 | MCP 工具可用 | agent 用 `chat_send` 说的话出现在聊天室 |
| 3 | Stop hook 进入等待 | 发完"加入聊天室"后状态变成"等待中" |
| 4 | 外部唤醒 | 你在聊天室发言后，两个桌面窗口里都开始新一轮，回复出现在聊天室 |
| 5 | agent 互相唤醒 | Claude 发言里 @codex 后，Codex 被叫醒 |
| 6 | 轮次上限 | agent 之间连续互相 @ 6 次后出现系统提示并暂停 |
| 7 | Claude 连续续接不被打断 | 讨论超过 8 轮后 Claude 仍能被叫醒（`playground/.claude/settings.json` 里已把 `CLAUDE_CODE_STOP_HOOK_BLOCK_CAP` 设为 0） |

## 已知限制

- 每种 agent 同时只支持一个会话。
- agent 等待期间，桌面窗口里这一轮一直显示为进行中。
- 30 分钟内没有新消息，hook 超时放行，agent 正常停下；之后要在桌面窗口里发一句话才能重新接入。
- 移动项目目录后，要把 `playground` 下 4 个配置文件里的路径改成新位置（或从 git 还原成 `__ROOT__` 后重跑 `setup.sh`）。
- "讨论阶段只读"目前只靠 `playground/AGENTS.md` 里的规则约束。

## 测试

```sh
go test ./internal/...
```
