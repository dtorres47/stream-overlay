#!/usr/bin/env bash
set -e

# 1. Build Go server binary
cd stream-overlay
go mod tidy
go build -o ../build/stream-overlay-server ./cmd/stream-overlay

# 2. Copy static assets
cd ..
rm -rf build/stream-overlay-web
mkdir -p build/stream-overlay-web
cp -R stream-overlay/web/* build/stream-overlay-web/

echo "✅ Build complete: build/stream-overlay-server + web files"
