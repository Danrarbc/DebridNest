#!/usr/bin/env bash
# Launch Stremio with developer tooling for addon/stream debugging.
#
# Usage:
#   ./scripts/stremio-debug.sh              # F12 devtools in Stremio
#   ./scripts/stremio-debug.sh remote       # Chrome inspect at :9222
#   ./scripts/stremio-debug.sh web          # Local stremio-web dev UI (run setup first)
#
set -euo pipefail

STREMIO_BIN="${STREMIO_BIN:-/Applications/Stremio.app/Contents/MacOS/Stremio}"
MODE="${1:-devtools}"
WEBUI_DIR="${STREMIO_WEB_DIR:-$HOME/apps/stremio-web}"
REMOTE_PORT="${STREMIO_DEBUG_PORT:-9222}"

if [[ ! -x "$STREMIO_BIN" ]]; then
  echo "Stremio not found at: $STREMIO_BIN" >&2
  echo "Set STREMIO_BIN to your Stremio executable." >&2
  exit 1
fi

echo "Stopping any running Stremio instances..."
pkill -x Stremio 2>/dev/null || true
sleep 1

case "$MODE" in
  devtools)
    echo "Starting Stremio with --dev-tools (press F12 for DevTools)."
    echo ""
    echo "When debugging DebridNest:"
    echo "  1. Open DevTools → Network tab → filter: stream"
    echo "  2. Open a title → Show Streams"
    echo "  3. Look for requests to your addon URL or /local-addon/"
    echo "  4. Check status code + response JSON (streams array length)"
    echo "  5. Console tab: filter for addon, stream, error"
    exec "$STREMIO_BIN" --dev-tools
    ;;
  remote)
    echo "Starting Stremio with remote debugging on port $REMOTE_PORT."
    echo "Open Chrome → http://127.0.0.1:$REMOTE_PORT → click 'inspect' on the Stremio page."
    export QTWEBENGINE_REMOTE_DEBUGGING="$REMOTE_PORT"
    export QTWEBENGINE_CHROMIUM_FLAGS="--remote-allow-origins=*"
    exec "$STREMIO_BIN" --dev-tools
    ;;
  web)
    if [[ ! -d "$WEBUI_DIR/node_modules" ]]; then
      echo "stremio-web not set up. Run: ./scripts/stremio-debug-setup.sh" >&2
      exit 1
    fi
    if ! lsof -i :8081 -P -n >/dev/null 2>&1; then
      echo "Starting stremio-web dev server on https://localhost:8081 ..."
      (cd "$WEBUI_DIR" && pnpm start) &
      WEB_PID=$!
      trap 'kill $WEB_PID 2>/dev/null || true' EXIT
      for i in {1..90}; do
        curl -sk --max-time 1 https://127.0.0.1:8081/ >/dev/null 2>&1 && break
        sleep 1
      done
    fi
    echo "Starting Stremio shell → local web UI at https://127.0.0.1:8081"
    echo "Press F12 in Stremio (with --dev-tools) or use Chrome at http://127.0.0.1:$REMOTE_PORT"
    export QTWEBENGINE_REMOTE_DEBUGGING="$REMOTE_PORT"
    exec "$STREMIO_BIN" --dev-tools --webui-url="https://127.0.0.1:8081/#loginFlow=desktop"
    ;;
  *)
    echo "Unknown mode: $MODE" >&2
    echo "Usage: $0 [devtools|remote|web]" >&2
    exit 1
    ;;
esac
