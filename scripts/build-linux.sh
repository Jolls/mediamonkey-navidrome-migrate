#!/usr/bin/env bash
# Cross-compile the migrate app for Linux into ./bin/migrate-linux.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"

out="bin/migrate-linux"
mkdir -p "$(dirname "$out")"

echo "building $out (GOOS=linux GOARCH=amd64) ..."
GOOS=linux GOARCH=amd64 go build -o "$out" ./cmd/app
echo "built $out"
