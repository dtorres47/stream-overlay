#!/usr/bin/env bash
set -e

#
# Universal build.sh for stream-overlay
# Works if you’re at:
#  • stream-overlay/ (your project root)
#  • portfolio-site/ (once you’ve copied stream-overlay/ under cmd/)
#

# 1. Locate the project root relative to this script
#    If you have a folder named "stream-overlay" next to this script, use that.
#    Otherwise assume this script is at the stream-overlay root.
if [ -d "$(dirname "$0")/stream-overlay" ]; then
  PROJECT_ROOT="$(dirname "$0")/stream-overlay"
else
  PROJECT_ROOT="$(dirname "$0")"
fi

# 2. Tidy & build the Go binary
cd "$PROJECT_ROOT"
go mod tidy
go build -o "$PROJECT_ROOT/build/stream-overlay-server" ./cmd/stream-overlay

# 3. Copy static web assets (entire tree)
rm -rf "$PROJECT_ROOT/build/stream-overlay-web"
mkdir -p "$PROJECT_ROOT/build/stream-overlay-web"
# IMPORTANT: copy directories recursively (fixes the cp error)
cp -a "$PROJECT_ROOT/cmd/stream-overlay/web/." "$PROJECT_ROOT/build/stream-overlay-web/"

echo "✅ Build complete:"
echo "   • Binary: build/stream-overlay-server"
echo "   • Static: build/stream-overlay-web/"
