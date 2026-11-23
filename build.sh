#!/bin/sh
# Builds dist/camera-<version>.tar.gz: cross-compiles the live view service for the robot (linux/arm64)
# and packs package/. Needs Go and GNU tar (Linux, WSL or Git Bash).
set -eu
cd "$(dirname "$0")"
VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' package/manifest.json)
GO=${GO:-go}
mkdir -p dist
(cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 "$GO" build -trimpath -ldflags "-s -w" -o ../dist/camera-live .)
OUT="dist/camera-$VERSION.tar.gz"
# Modes are set by tar: a Windows drive does not keep them.
tar --owner=0 --group=0 --mode=0644 --exclude=handler -cf dist/pkg.tar -C package .
tar --owner=0 --group=0 --mode=0755 -rf dist/pkg.tar -C package handler
tar --owner=0 --group=0 --mode=0755 -rf dist/pkg.tar -C dist camera-live
gzip -9 -c dist/pkg.tar > "$OUT" && rm dist/pkg.tar dist/camera-live
sha256sum "$OUT"
