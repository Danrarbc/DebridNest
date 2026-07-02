#!/usr/bin/env bash
# One-time setup: clone and build Stremio Web UI from source for local debugging.
#
# The desktop shell (Qt) is NOT required — use Stremio.app with --webui-url instead.
# Streaming server still runs from Stremio.app on port 11470.
#
set -euo pipefail

WEBUI_DIR="${STREMIO_WEB_DIR:-$HOME/apps/stremio-web}"
NODE_MAJOR="${STREMIO_NODE_MAJOR:-20}"

echo "==> Stremio debug setup"
echo "Target: $WEBUI_DIR"

if command -v nvm >/dev/null 2>&1; then
  # shellcheck disable=SC1090
  source "$(brew --prefix nvm 2>/dev/null)/nvm.sh" 2>/dev/null || true
fi

if command -v nvm >/dev/null 2>&1; then
  nvm install "$NODE_MAJOR" >/dev/null
  nvm use "$NODE_MAJOR"
fi

PNPM="$(command -v pnpm || true)"
if [[ -z "$PNPM" && -x "$HOME/.nvm/versions/node/$(node -v | tr -d v)/bin/pnpm" ]]; then
  PNPM="$HOME/.nvm/versions/node/$(node -v | tr -d v)/bin/pnpm"
fi
if [[ -z "$PNPM" ]]; then
  echo "pnpm not found. Install Node 20+ with pnpm (npm i -g pnpm)." >&2
  exit 1
fi

echo "Node: $(node -v)"
echo "pnpm: $PNPM"

if [[ ! -d "$WEBUI_DIR/.git" ]]; then
  git clone --depth 1 https://github.com/Stremio/stremio-web.git "$WEBUI_DIR"
fi

cd "$WEBUI_DIR"
"$PNPM" install

echo ""
echo "Setup complete."
echo ""
echo "Next steps:"
echo "  1. ./scripts/stremio-debug.sh devtools   # F12 in Stremio (fastest)"
echo "  2. ./scripts/stremio-debug.sh remote     # Chrome inspect at :9222"
echo "  3. ./scripts/stremio-debug.sh web        # Local stremio-web + devtools"
echo ""
echo "Addon install URL (example — use your configure page URL):"
echo "  https://stremio.136.244.66.44.sslip.io/{config}/manifest.json"
