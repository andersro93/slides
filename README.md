<div align="center">

# Slides

**Present from any computer with a browser.**

[slides.ros-nett.com](https://slides.ros-nett.com)

</div>

---

Walk into any meeting room, open slides.ros-nett.com, type the
presentation's code, and present. Nothing to install, no USB stick, no
logging in on someone else's laptop.

- **One text field.** The landing page is a single input. The code *is* the
  access: there is no listing of presentations anywhere, and wrong guesses
  are rate-limited.
- **reveal.js underneath.** Presentations are Markdown or reveal.js HTML
  ([authoring guide](docs/authoring.md)), with speaker notes, fragments, code
  highlighting, themes and PDF export.
- **Content lives outside the image.** The presentations are a folder you
  mount (plain directory, or a private git repo via git-sync). New and edited
  decks go live within seconds, no release or restart. A deck that breaks
  keeps serving its last good version.
- **Your phone is the clicker.** Press **R** on the deck, scan the QR code,
  and your phone shows next/previous, your speaker notes, the next slide and
  a timer. The QR is single-use, so someone photographing the projector gets
  nothing.
- **One small image.** A static Go binary with the frontend embedded
  (`go:embed`), on distroless: about 6 MB. No database, nothing to write.

## Working on it

Toolchain via [mise](https://mise.jdx.dev): `mise install`, then `bun install`.

| | |
| --- | --- |
| `mise run dev` | Server on :3000 for `DECKS_DIR` (default `examples/`); a saved edit reloads the open deck, drafts are served |
| `mise run validate [dir]` | Check a presentations folder (default `examples/`) |
| `mise run test` | Go (with `-race`) and frontend unit tests |
| `mise run check` | Biome, TypeScript, golangci-lint, GoReleaser config |
| `mise run e2e` | Playwright against a local server and `e2e/fixtures` |
| `mise run artifacts` | `dist/server/linux/{amd64,arm64}/slides`, frontend embedded |
| `mise run image` | Multi-arch image via buildx |

Single-arch local image: `mise run artifacts && docker build -t slides:dev .`

Author against your real presentations with
`DECKS_DIR=~/code/presentations mise run dev`.

### Layout

```
apps/server/     Go: cmd/slides (serve | validate | list | healthcheck | version)
  internal/decks     frontmatter parsing, validation, the watched Source
  internal/web       routes, headers, per-client miss limiter
  internal/remote    phone-remote pairing and relay (WebSockets)
  internal/embedded  go:embed of the built frontend
apps/frontend/   Vite + TypeScript, no framework: landing, deck, remote
examples/        two demo decks (Markdown, HTML) for `mise run dev`
docs/            authoring guide
e2e/             Playwright + its fixture decks (also the CI smoke test's)
deploy/          compose example: Cloudflare Tunnel, folder or git-sync
```

### Routes

| | |
| --- | --- |
| `/` | the code field |
| `/<code>/` | the deck |
| `/<code>/_remote` | the phone remote |
| `/<code>/_ws` | remote WebSocket |
| `/<code>/<file>` | files from the deck's directory (never its `index.*`) |
| `/_app/…` | hashed frontend assets, cached immutable |
| `/healthz`, `/robots.txt` | |

## Running it

```sh
docker run -p 3000:3000 -v ./presentations:/decks:ro ghcr.io/andersro93/slides:latest
```

Without a mount it refuses to start. [`deploy/compose.yaml`](deploy/compose.yaml)
runs it behind Cloudflare Tunnel, with the presentations as a host folder or
a private git repository kept in sync by git-sync.

| Env | Default | |
| --- | --- | --- |
| `DECKS_DIR` | `/decks` (image) | The presentations folder. Watched; `SIGHUP` forces a rescan. |
| `RELOAD_INTERVAL` | `5s` (`1s` with `DEV`) | How often the folder is checked for changes |
| `CLIENT_IP_HEADER` | *(peer address)* | Header holding the real client IP, e.g. `CF-Connecting-IP` behind Cloudflare. Only set it when the proxy is the only way in. |
| `PORT` | `3000` | |
| `DEV` | off | Authoring: serve `draft: true` decks, open decks reload themselves, an unknown code shows the load errors |
| `APP_DIR` | *(embedded)* | Serve the frontend from disk (frontend development) |

Check a presentations folder before it goes live (e.g. in the content
repo's CI): `docker run --rm -v "$PWD:/decks:ro" ghcr.io/andersro93/slides validate`.

## Security model

- The code is the only credential; anyone with it sees the deck. Private
  decks need `private: true` and a ≥ 12-character code. After 30 misses in 10
  minutes a client gets 429 on every deck route, including valid codes.
- Everything is `noindex`, `robots.txt` disallows all, `Referrer-Policy:
  no-referrer` keeps codes out of other sites' logs, and deck files are
  `Cache-Control: private`.
- The image contains no presentations, so it is not sensitive; the mounted
  folder (or its git repository) is. The server only follows plain files:
  symlinks inside the folder are refused, so it cannot be tricked into
  serving anything from outside it.
- Remote pairing: the QR token is single-use; the paired phone gets its own
  key for reconnecting; the deck holds a separate host key. The server only
  relays `cmd` from phone to deck and `state` from deck to phone. Anyone with
  a code can run a host session, so the phone treats notes as untrusted
  (DOMPurify, and a remote-page CSP without inline script), and sessions are
  capped per client (8) and overall (256); abandoned unpaired sessions expire
  after 2 minutes. Sessions live in memory, so a restart means scanning again.
- Rate limiting keys on the IPv4 address or the IPv6 /64.

## Releases

Same flow as pjokk: Conventional Commits drive
[svu](https://github.com/caarlos0/svu). A PR builds, smoke-tests,
browser-tests and pushes a preview image (`0.3.0-pr.12`); **merging to main
releases**: a tag, a GitHub Release with changelog, and
`ghcr.io/andersro93/slides:{version,major.minor,major,latest}` for
amd64 + arm64. Presentations are not part of this cycle at all.
