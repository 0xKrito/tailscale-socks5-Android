#!/bin/bash
# Apply Android-specific patches to tailscale source
# Usage: ./patch.sh [path-to-tailscale-source]
#   If no path given, auto-detects from go module cache
set -e

MY_DIR=$(cd $(dirname $0);pwd)

if [ -n "$1" ]; then
    TS_DIR="$1"
else
    TS_DIR=$(go list -m -f '{{.Dir}}' tailscale.com 2>/dev/null) || true
    if [ -z "$TS_DIR" ]; then
        echo "ERROR: tailscale.com not found. Run 'go mod download' first."
        exit 1
    fi
fi
echo "Tailscale source: $TS_DIR"

# 1. Patch cert.go + disabled_stubs.go (enable ACME on Android)
cd "$TS_DIR"
if ! grep -s "//go:build" ipn/localapi/cert.go | grep -q "android"; then
    echo "Patching cert.go + disabled_stubs.go..."
    patch -p0 < "$MY_DIR/cert.go.patch" || true
    patch -p0 < "$MY_DIR/disabled_stubs.go.patch" || true
fi

# 2. Patch tsnet/tsnet.go: add Android fallback for os.Executable()
if ! grep -q '"android"' tsnet/tsnet.go; then
    echo "Patching tsnet/tsnet.go for Android..."
    sed -i '/case "ios":/a\\t\tcase "android":\n\t\t\texe = "tsnet"' tsnet/tsnet.go
fi

echo "Android patches applied"
