# Slides — notes for agents

reveal.js presentations served by one Go binary. The frontend is embedded;
the presentations are NOT in this repo or the image — they are a mounted
folder (DECKS_DIR), watched and hot-reloaded. Read README.md first;
docs/authoring.md is the authoring guide, examples/ holds demo decks and
e2e/fixtures the test decks.

## Rules

- **Conventional Commits, always.** svu computes release versions from them
  and GoReleaser builds the changelog from them. Merging to main releases.
- Toolchain comes from `.mise.toml`; do not add tools CI does not pin.
- `mise run check` and `mise run test` must pass. Go: `golangci-lint run`
  from apps/server; errcheck is on.
- The Dockerfile is COPY-only. Never compile inside it; build natively via
  scripts/build-artifacts.sh.
- `apps/server/internal/embedded/app` is filled by scripts/embed.sh and
  gitignored; never commit its contents.
- The running server must never go down because of content: decks.Reload
  keeps a broken deck's last good version; only an unreadable DECKS_DIR is
  fatal (at startup).
- A deck theme is a file in apps/frontend/src/themes AND an entry in
  `decks.Themes` (a Go test keeps them in step).
- The WebSocket protocol lives in apps/server/internal/remote (server) and
  apps/frontend/src/lib/protocol.ts (clients). Change both together; the hub
  enforces direction (host → state, remote → cmd). Host state is untrusted
  on the phone (anyone with a code can host): keep notes sanitized.
- Codes must never become enumerable: no listing endpoint, no sitemap, no
  distinguishable responses for "exists but …".
