#!/usr/bin/env bash
# Refresh the writable working copies from the read-only master DBs.
#
# local/masters/  = pristine originals, chmod 444, never written (git-ignored).
# local/work/     = throwaway copies the app/tests point at (git-ignored).
#
# Run this before a dry-run or a real run so every run starts from a known,
# clean state. Safe to run any time; it overwrites local/work/.
set -euo pipefail

repo="$(cd "$(dirname "$0")/.." && pwd)"
masters="$repo/local/masters"
work="$repo/local/work"

if [[ ! -d "$masters" ]]; then
  echo "no masters at $masters — put the original MM5.DB / navidrome.db there" >&2
  exit 1
fi

mkdir -p "$work"
for f in "$masters"/*; do
  name="$(basename "$f")"
  cp -f "$f" "$work/$name"
  chmod 644 "$work/$name"   # working copies are writable
  echo "reset $work/$name"
done
