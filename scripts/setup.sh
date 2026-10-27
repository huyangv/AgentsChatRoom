#!/bin/sh
# 首次搭建：用 Wails 官方 Vue 模板补齐工程骨架（build 目录、Taskfile、go.mod 等），
# 安装前端依赖，并构建给 agent 用的 chatroom-bridge。已有文件不会被覆盖。
#
# 需要：Go 1.26 及以上（或允许 Go 自动下载工具链）、Node.js、wails3 命令。
set -eu

ROOT=$(cd "$(dirname "$0")/.." && pwd)

if ! command -v wails3 >/dev/null 2>&1; then
  echo "找不到 wails3，请先执行：go install github.com/wailsapp/wails/v3/cmd/wails3@v3.0.0"
  exit 1
fi

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
wails3 init -n AgentChatRoom -t vue -mod agentchatroom -d "$TMP" -skipgomodtidy -q

# 模板里的示例代码本项目不用
rm "$TMP/AgentChatRoom/greetservice.go" "$TMP/AgentChatRoom/frontend/src/components/HelloWorld.vue"

# 只补充缺少的文件，不覆盖本项目已有的代码。
# 不用 cp -n：macOS 上它跳过已有文件时返回非零，会让脚本中止。
(cd "$TMP/AgentChatRoom" && find . -type f) | while IFS= read -r f; do
  if [ ! -e "$ROOT/$f" ]; then
    mkdir -p "$ROOT/$(dirname "$f")"
    cp -p "$TMP/AgentChatRoom/$f" "$ROOT/$f"
  fi
done

cd "$ROOT"
go mod tidy
(cd frontend && npm install naive-ui@^2.45.3)
go build -o bin/chatroom-bridge ./cmd/chatroom-bridge

# 把 playground 配置里的 __ROOT__ 换成本项目的实际路径
for f in playground/.mcp.json playground/.claude/settings.json playground/.codex/config.toml playground/.codex/hooks.json; do
  sed "s#__ROOT__#$ROOT#g" "$f" > "$f.tmp" && mv "$f.tmp" "$f"
done

echo "搭建完成。启动聊天室：cd \"$ROOT\" && wails3 dev"
