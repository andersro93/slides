# Security policy

A deck's code is its only credential, so bugs that leak, guess or bypass
codes are security bugs here, not just correctness bugs.

## Reporting a vulnerability

Please **do not open a public issue.** Report it privately through
[GitHub's private vulnerability reporting](https://github.com/andersro93/slides/security/advisories/new).
Include what you did, what you expected, and what happened; a proof of
concept against a local `mise run dev` is ideal. Never test against someone
else's instance.

You can expect an acknowledgement within a week. Fixes ship as a normal
release (merging to main releases), and the advisory is published once an
image with the fix is on ghcr.io.

## Supported versions

Only the latest release is supported. There are no long-lived release
branches; upgrade by pulling `ghcr.io/andersro93/slides:latest` or the newest
version tag.

## In scope

The threat model is described in the README's
[security model](README.md#security-model). In particular:

- **Enumerating or confirming codes**: any response, timing or header that
  distinguishes "exists but …" from "does not exist", or a way around the
  per-client miss limiter.
- **Serving files outside a deck's directory**: path traversal, symlinks, or
  reading a deck's `index.*` source directly.
- **Phone-remote pairing**: reusing a QR token, hijacking a paired session,
  sending `state` from a phone or `cmd` from a deck, or script execution on
  the remote page via speaker notes.
- **Leaking codes to third parties**: through referrers, caches or indexing.
- **Taking the server down with content**: a deck that crashes the process
  or stops other decks from being served.

## Out of scope

- Anyone who has a code can see that deck. That is the design; use
  `private: true` with a long code for sensitive decks.
- Brute force from a large pool of IP addresses. Rate limiting is per client
  (IPv4 address or IPv6 /64); put a WAF or rate limiter in front if that
  matters for you.
- A misconfigured `CLIENT_IP_HEADER` on a server that is reachable without
  the proxy in front of it.
- Script in a deck's own HTML. Deck authors are trusted: whoever can write to
  the presentations folder controls what the deck page runs.
