# syntax=docker/dockerfile:1

# Slides — one static Go binary with the frontend embedded (go:embed). The
# presentations are NOT in the image: mount them at /decks, read-only.
#
#   docker run -p 3000:3000 -v ./presentations:/decks:ro ghcr.io/andersro93/slides
#
# The folder is watched; new and edited decks go live without a restart.
# Modes by argv[1]: serve (default), validate [dir], healthcheck, version —
# see apps/server/cmd/slides/main.go.
#
# NOTHING COMPILES IN HERE. The binaries are built natively, outside Docker:
#
#   bash scripts/build-artifacts.sh   # → dist/server/linux/{amd64,arm64}/slides
#
# and this file only COPYs the one matching TARGETPLATFORM, so a multi-arch
# buildx run is seconds of copying with no QEMU. If the COPY fails with
# "not found", run the script first.
#
# distroless static: no shell, no libc, no package manager; ships CA certs,
# tzdata and the nonroot user (uid 65532). Pinned by digest; Dependabot
# bumps it.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

ARG TARGETPLATFORM
# dist/server for build-artifacts.sh; GoReleaser's dockers_v2 passes ".".
ARG BINARY_ROOT=dist/server

COPY ${BINARY_ROOT}/${TARGETPLATFORM}/slides /app/slides

ENV PORT=3000 \
    DECKS_DIR=/decks
EXPOSE 3000

# The binary is its own healthcheck client; there is no curl here.
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD ["/app/slides", "healthcheck"]

ENTRYPOINT ["/app/slides"]
