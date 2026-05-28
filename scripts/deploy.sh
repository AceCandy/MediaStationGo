#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
PORT="${PORT:-18080}"
HOST="${HOST:-0.0.0.0}"
DATA_DIR="${DATA_DIR:-$ROOT_DIR/data}"
CACHE_DIR="${CACHE_DIR:-$ROOT_DIR/cache}"
WEB_DIR="${WEB_DIR:-$ROOT_DIR/web/dist}"
BIN_DIR="${BIN_DIR:-$ROOT_DIR/bin}"
BIN_PATH="${BIN_PATH:-$BIN_DIR/mediastation-go}"
PID_FILE="${PID_FILE:-$ROOT_DIR/.mediastation.pid}"
LOG_FILE="${LOG_FILE:-$ROOT_DIR/logs/mediastation.log}"
SKIP_BUILD="${SKIP_BUILD:-0}"
DETACH="${DETACH:-1}"

usage() {
  cat <<EOF
MediaStationGo one-click deploy

Usage:
  PORT=18080 DATA_DIR=/opt/mediastation/data ./scripts/deploy.sh

Environment:
  PORT          HTTP port, default 18080
  HOST          Listen host, default 0.0.0.0
  DATA_DIR      Persistent data directory, default ./data
  CACHE_DIR     Cache directory, default ./cache
  WEB_DIR       Built frontend directory, default ./web/dist
  SKIP_BUILD    1 to skip npm/go build
  DETACH        1 to run in background, 0 to run in foreground
EOF
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
  usage
  exit 0
fi

mkdir -p "$DATA_DIR" "$CACHE_DIR" "$BIN_DIR" "$(dirname "$LOG_FILE")"

if [[ "$SKIP_BUILD" != "1" ]]; then
  echo "[1/3] Building frontend"
  (cd "$ROOT_DIR/web" && npm ci && npm run build)
  echo "[2/3] Building server"
  (cd "$ROOT_DIR" && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o "$BIN_PATH" ./cmd/server)
else
  echo "[1/3] Build skipped"
fi

if [[ -f "$PID_FILE" ]]; then
  old_pid="$(cat "$PID_FILE" || true)"
  if [[ -n "$old_pid" ]] && kill -0 "$old_pid" 2>/dev/null; then
    echo "[3/3] Stopping old process $old_pid"
    kill "$old_pid" || true
    sleep 1
  fi
fi

export MEDIASTATION_APP_HOST="$HOST"
export MEDIASTATION_APP_PORT="$PORT"
export MEDIASTATION_APP_DATA_DIR="$DATA_DIR"
export MEDIASTATION_APP_WEB_DIR="$WEB_DIR"
export MEDIASTATION_DATABASE_DB_PATH="$DATA_DIR/mediastation.db"
export MEDIASTATION_CACHE_CACHE_DIR="$CACHE_DIR"

echo "[3/3] Starting MediaStationGo on http://$HOST:$PORT"
if [[ "$DETACH" == "1" ]]; then
  nohup "$BIN_PATH" >>"$LOG_FILE" 2>&1 &
  echo $! > "$PID_FILE"
  sleep 2
  if command -v curl >/dev/null 2>&1; then
    curl -fsS "http://127.0.0.1:$PORT/api/health" >/dev/null
  fi
  echo "Started. PID=$(cat "$PID_FILE"), log=$LOG_FILE"
else
  exec "$BIN_PATH"
fi
