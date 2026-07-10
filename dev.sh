#!/usr/bin/env bash
# 本地开发一键启动：同时拉起 Go 后端和前端 vite 开发服务器，Ctrl+C 退出。
# 配置全部读根目录 .env（从 .env.example 复制），脚本本身不内置业务默认值。

set -euo pipefail

cd "$(dirname "$0")"

if [[ ! -f .env ]]; then
  echo "未找到 .env，请先复制：cp .env.example .env"
  exit 1
fi
# 剥离可能的 Windows 回车符(CRLF)，避免 source 时报 $'\r': command not found。
set -a; source <(tr -d '\r' < .env); set +a

command -v go >/dev/null  || { echo "缺少 go，请先安装 Go 1.25+"; exit 1; }
command -v npm >/dev/null || { echo "缺少 npm，请先安装 Node.js"; exit 1; }
[[ -d web/node_modules ]] || npm --prefix web install

# 子进程清理：Ctrl+C 或异常退出时一并关掉后端与前端。
BACK_PID=""; FRONT_PID=""
cleanup() {
  [[ -n "$BACK_PID" ]]  && kill "$BACK_PID"  2>/dev/null || true
  [[ -n "$FRONT_PID" ]] && kill "$FRONT_PID" 2>/dev/null || true
  wait 2>/dev/null || true
}
trap cleanup EXIT INT TERM

echo "------------------------------------------------------------"
echo " 后端  http://127.0.0.1:${MEDIASTATION_APP_PORT:-6201}   (健康检查 /api/health)"
echo " 前端  http://127.0.0.1:6200"
echo " Ctrl+C 退出"
echo "------------------------------------------------------------"

go run ./cmd/server &
BACK_PID=$!

# 前端端口/局域网访问固化在 web/vite.config.ts（port 6200，strictPort，host: true）。
npm --prefix web run dev &
FRONT_PID=$!

wait
