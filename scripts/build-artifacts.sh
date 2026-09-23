#!/usr/bin/env bash
# Builds everything the container image COPYs, natively — nothing compiles
# inside Docker. The frontend is embedded; presentations are mounted at
# runtime and never part of the build. Output:
#
#   dist/server/linux/amd64/slides
#   dist/server/linux/arm64/slides
#
# The layout mirrors GoReleaser's dockers_v2 build context
# (linux/<arch>/slides), so one Dockerfile COPY line serves both this script
# (BINARY_ROOT=dist/server, the default) and GoReleaser (BINARY_ROOT=.).
#
# Prerequisites: `mise install` (Go + Bun) and `bun install`.
set -euo pipefail
cd "$(dirname "$0")/.."

bash scripts/embed.sh

# The version stamped into the binary (internal/buildinfo). CI passes the
# preview image's pinned tag; a local build says "dev".
VERSION="${SLIDES_VERSION:-dev}"

echo "==> server binaries ($VERSION)"
rm -rf dist/server
for arch in amd64 arm64; do
  mkdir -p "dist/server/linux/$arch"
  (cd apps/server && CGO_ENABLED=0 GOOS=linux GOARCH="$arch" \
    go build -trimpath \
    -ldflags="-s -w -X github.com/andersro93/slides/server/internal/buildinfo.Version=$VERSION" \
    -o "../../dist/server/linux/$arch/slides" ./cmd/slides)
done
ls -lh dist/server/linux/*/
