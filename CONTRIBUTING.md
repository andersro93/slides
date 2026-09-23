# Contributing

Thanks for taking an interest! Bug reports, deck-rendering problems and pull
requests are all welcome. For anything bigger than a fix, please open an
issue first so we can agree on the approach before you spend time on it.

Found a security problem? See [SECURITY.md](SECURITY.md) instead.

## Getting set up

The toolchain (Go, Bun, golangci-lint, GoReleaser, svu) is pinned in
[`.mise.toml`](.mise.toml). With [mise](https://mise.jdx.dev) installed:

```sh
mise install
bun install
mise run dev        # server on :3000 serving examples/
```

The README's [Working on it](README.md#working-on-it) section lists every
task and describes the layout.

## Before you open a pull request

```sh
mise run check      # Biome, TypeScript, golangci-lint, GoReleaser config
mise run test       # Go (with -race) and frontend unit tests
mise run e2e        # Playwright, for anything user-visible
```

CI runs the same suite, then builds the image, smoke-tests it and runs the
end-to-end tests against it.

## Pull request titles: Conventional Commits

**Merging to main releases.** Pull requests are squash-merged, and the PR
title becomes the commit message that [svu](https://github.com/caarlos0/svu)
reads to pick the next version and that the changelog is built from. A check
enforces the format:

```
<type>[optional scope]: <description>
```

| Type | Releases | Use for |
| --- | --- | --- |
| `feat` | minor | new behaviour users can see |
| `fix` | patch | bug fixes |
| `perf` | patch | performance improvements |
| `docs`, `test`, `refactor`, `build`, `ci`, `chore`, `style` | nothing | everything else |

Add `!` after the type (`feat!: …`) for a breaking change, for example to the
deck frontmatter, the routes, or the environment variables.

Commits inside your branch can say anything; only the title counts.

## Things that are easy to miss

- **The server must not go down because of content.** A broken deck keeps
  serving its last good version; only an unreadable `DECKS_DIR` at startup is
  fatal.
- **Codes must never become enumerable.** No listing endpoints, and no
  responses that tell "exists but …" apart from "not found".
- **The WebSocket protocol has two halves**: `apps/server/internal/remote` and
  `apps/frontend/src/lib/protocol.ts`. Change them together.
- **A theme is two things**: a file in `apps/frontend/src/themes` and an
  entry in `decks.Themes`. A Go test keeps them in step.
- **The Dockerfile only COPYs.** Binaries are built natively by
  `scripts/build-artifacts.sh`; never add a compile step to the image.
- Don't add tools that `.mise.toml` doesn't pin.

## License

By contributing, you agree that your contributions are licensed under the
[MIT License](LICENSE).
