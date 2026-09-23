<!--
The PR title becomes the squash-merge commit and decides the release:
  feat: …  → minor    fix: … / perf: …  → patch    docs/test/chore/ci/refactor: … → no release
Add ! for breaking changes (feat!: …). See CONTRIBUTING.md.
-->

## What and why

## How it was tested

- [ ] `mise run check`
- [ ] `mise run test`
- [ ] `mise run e2e` (user-visible changes)
- [ ] Docs updated (README, `docs/authoring.md`) if behaviour changed
