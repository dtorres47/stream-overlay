#!/usr/bin/env bash
set -euo pipefail

# ===== Resolve project root =====
# This script can live at:
# - repo root that contains cmd/stream-overlay
# - or at stream-overlay/ itself (which contains cmd/stream-overlay)
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

if [[ -d "$SCRIPT_DIR/cmd/stream-overlay" ]]; then
  PROJECT_ROOT="$SCRIPT_DIR"
elif [[ -d "$SCRIPT_DIR/stream-overlay/cmd/stream-overlay" ]]; then
  PROJECT_ROOT="$SCRIPT_DIR/stream-overlay"
else
  echo "❌ Could not locate cmd/stream-overlay relative to $SCRIPT_DIR"
  exit 1
fi

APP_DIR="$PROJECT_ROOT/cmd/stream-overlay"
WEB_DIR="$APP_DIR/web"
BUILD_DIR="$PROJECT_ROOT/build"
BIN_PATH="$BUILD_DIR/stream-overlay-server"
WEB_OUT="$BUILD_DIR/stream-overlay-web"

echo "ℹ️  PROJECT_ROOT: $PROJECT_ROOT"
echo "ℹ️  APP_DIR     : $APP_DIR"
echo "ℹ️  WEB_DIR     : $WEB_DIR"
echo "ℹ️  BUILD_DIR   : $BUILD_DIR"

# ===== Go build =====
pushd "$PROJECT_ROOT" >/dev/null
go mod tidy
go build -o "$BIN_PATH" ./cmd/stream-overlay
popd >/dev/null

# ===== Copy static web assets =====
rm -rf "$WEB_OUT"
mkdir -p "$WEB_OUT"

# Use cp -a (archive) to preserve tree & copy directories recursively.
# The trailing '/.' ensures contents are copied, not the folder itself.
cp -a "$WEB_DIR/." "$WEB_OUT/"

# If your Actions runner might not have 'cp -a', uncomment this rsync alternative:
# rsync -av --delete "$WEB_DIR/" "$WEB_OUT/"

echo "✅ Build complete:"
echo "   • Binary: $BIN_PATH"
echo "   • Static: $WEB_OUT/"
