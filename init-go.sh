#!/usr/bin/env bash
# Use with Replit

GO_VERSION=1.20.7
INSTALL_DIR="$HOME/go_toolchain"

# If already installed, skip
if [ -x "$INSTALL_DIR/bin/go" ] && [[ "$($INSTALL_DIR/bin/go version)" == *"go$GO_VERSION"* ]]; then
  export PATH="$INSTALL_DIR/bin:$PATH"
  return
fi

# Download and unpack
rm -rf "$INSTALL_DIR"
mkdir -p "$INSTALL_DIR"
curl -sSL "https://golang.org/dl/go${GO_VERSION}.linux-amd64.tar.gz" | tar -C "$INSTALL_DIR" --strip-components=1 -xz

export PATH="$INSTALL_DIR/bin:$PATH"
