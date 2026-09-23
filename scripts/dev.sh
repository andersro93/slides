#!/usr/bin/env bash
# Local authoring loop: http://localhost:3000
#
#   DECKS_DIR=~/path/to/presentations mise run dev
#
# (defaults to ./examples). The frontend rebuilds on change (vite build
# --watch → dist/app, served via APP_DIR), DEV=1 serves drafts, and an open
# deck reloads itself within a second or two of saving its source.
set -euo pipefail
cd "$(dirname "$0")/.."

DECKS_DIR="$(cd "${DECKS_DIR:-examples}" && pwd)"
echo "==> presentations from $DECKS_DIR"

bun run build >/dev/null
bun run --filter @slides/frontend dev &
VITE_PID=$!
trap 'kill $VITE_PID 2>/dev/null || true' EXIT

cd apps/server
APP_DIR=../../dist/app DECKS_DIR="$DECKS_DIR" DEV=1 PORT="${PORT:-3000}" \
  go run ./cmd/slides serve
