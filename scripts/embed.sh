#!/usr/bin/env bash
# Builds the frontend and copies it into the binary's go:embed directory
# (apps/server/internal/embedded/app). That directory is gitignored except
# for its .gitkeep, so this never dirties the working tree. Run before any
# `go build` that should produce a servable binary (build-artifacts.sh and
# GoReleaser do).
#
# Presentations are NOT embedded — the server reads them from DECKS_DIR.
#
# SKIP_FRONTEND_BUILD=1 reuses an existing dist/app.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ "${SKIP_FRONTEND_BUILD:-0}" != "1" ] || [ ! -f dist/app/index.html ]; then
  echo "==> frontend"
  bun run build
fi

embed=apps/server/internal/embedded/app
find "$embed" -mindepth 1 ! -name .gitkeep -exec rm -rf {} +
cp -R dist/app/. "$embed/"
echo "==> embedded into $embed"
