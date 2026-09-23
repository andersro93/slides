#!/usr/bin/env bash
# Multi-arch image (linux/amd64 + linux/arm64) via buildx.
#
#   bash scripts/build-image.sh                                  # verify only
#   TAG=ghcr.io/andersro93/slides:dev PUSH=1 bash scripts/build-image.sh
#
# The binaries are compiled natively first (build-artifacts.sh; skipped with
# SKIP_ARTIFACTS=1 when dist/server exists). The Dockerfile is COPY-only, so
# the multi-platform step is seconds of copying, with no QEMU.
#
# Single-arch local iteration: `docker build -t slides:dev .` after running
# build-artifacts.sh once.
set -euo pipefail
cd "$(dirname "$0")/.."

TAG="${TAG:-slides:dev}"
PLATFORMS="${PLATFORMS:-linux/amd64,linux/arm64}"

if [ "${SKIP_ARTIFACTS:-0}" != "1" ] || [ ! -e dist/server/linux/amd64/slides ]; then
  bash scripts/build-artifacts.sh
fi

# Only the docker-container driver builds several platforms in one go.
BUILDER="${BUILDER:-slides}"
if ! docker buildx inspect "$BUILDER" >/dev/null 2>&1; then
  docker buildx create --name "$BUILDER" --driver docker-container >/dev/null
fi

if [ "${PUSH:-0}" = "1" ]; then
  OUTPUT=(--push)
else
  # A manifest list cannot live in the local image store: verify, discard.
  OUTPUT=(--output=type=cacheonly)
fi

docker buildx build --builder "$BUILDER" --platform "$PLATFORMS" \
  --tag "$TAG" "${OUTPUT[@]}" .
