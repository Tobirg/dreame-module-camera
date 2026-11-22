#!/bin/sh
# Builds dist/camera-<version>.tar.gz from package/. Needs GNU tar (Linux, WSL or Git Bash).
set -eu
cd "$(dirname "$0")"
VERSION=$(sed -n 's/.*"version": *"\([^"]*\)".*/\1/p' package/manifest.json)
mkdir -p dist
OUT="dist/camera-$VERSION.tar.gz"
# Modes are set by tar: a Windows drive does not keep them.
tar --owner=0 --group=0 --mode=0644 --exclude=handler -cf dist/pkg.tar -C package .
tar --owner=0 --group=0 --mode=0755 -rf dist/pkg.tar -C package handler
gzip -9 -c dist/pkg.tar > "$OUT" && rm dist/pkg.tar
sha256sum "$OUT"
