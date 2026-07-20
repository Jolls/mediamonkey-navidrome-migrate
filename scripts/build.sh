#!/usr/bin/env bash
# Build the migrate app into ./bin/migrate.exe.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo"

out="bin/migrate.exe"
mkdir -p "$(dirname "$out")"

echo "building $out ..."
go build -o "$out" ./cmd/app
echo "built $out"
