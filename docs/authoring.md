# Writing presentations

The server reads a presentations folder (`DECKS_DIR`, mounted at `/decks`
in the container). One directory per presentation. The directory name is
only for you; the **code** in the frontmatter is what people type and what
goes in the URL (`https://slides.ros-nett.com/<code>/`).

```
presentations/
  kubecon-2026/
    index.md          ← or index.html, never both
    diagram.png       ← anything the deck references, relatively
  _template/          ← names starting with _ or . are ignored
```

Changes go live within seconds of landing in the folder. A deck with an
error is not taken down: it keeps its last good version, and the server
logs what is wrong. Check a folder before publishing with
`mise run validate path/to/presentations`, or with the image itself:
`docker run --rm -v "$PWD:/decks:ro" ghcr.io/andersro93/slides validate`.

While writing, `DECKS_DIR=path/to/presentations mise run dev` serves drafts
and reloads the open deck on every save. [`examples/`](../examples) has a
Markdown and an HTML deck to start from. Plain files only: symlinks are
refused.

## Frontmatter

Both formats start with a YAML block:

```yaml
---
code: kubecon26            # required: 3–64 of a-z, 0-9 and inner hyphens
title: Scaling the thing   # required: the browser tab title
description: One line.     # optional
theme: ros                 # optional; ros (default) or any reveal theme:
                           #   black white league beige night serif simple
                           #   solarized moon dracula sky blood
                           #   black-contrast white-contrast
css: [style.css]           # optional; stylesheets inside this directory
private: true              # optional; requires a code of ≥ 12 characters
draft: true                # optional; only served in dev mode (DEV=1)
reveal:                    # optional; passed straight to reveal.js
  transition: fade         #   https://revealjs.com/config/
  slideNumber: false
---
```

Unknown keys are an error, so a typo never silently changes a deck.

**Public vs private.** Every deck is reachable only by its code; there is no
listing anywhere. For a talk you give on stage, pick something memorable
(`kubecon26`). For a private meeting, pick something unguessable
(`mint-orbit-falcon-42`) and mark it `private: true`, which enforces the
length. Wrong guesses are rate-limited per visitor.

## Markdown (`index.md`)

```markdown
# First slide

Note:
Speaker notes: shown in the speaker view (S) and on the phone remote (R).

---

## Next slide

- A line with only `---` starts a new slide
- A line with only `--` starts a vertical slide below

--

## Vertical slide

![Diagram](diagram.png)
```

Reveal's Markdown extras work: `<!-- .slide: data-background-color="#fff" -->`,
`<!-- .element: class="fragment" -->`, code blocks with line highlights
(```` ```go [1-2|4] ````).

## HTML (`index.html`)

After the frontmatter, write reveal.js `<section>` markup directly: nested
sections are vertical slides, `<aside class="notes">` holds speaker notes.
Inline `<script>` tags do not run.

## Presenting

| Key | |
| --- | --- |
| R | Control from your phone (shows a single-use QR code) |
| S | Speaker view (separate window) |
| F | Fullscreen |
| O / Esc | Overview |
| B / . | Blackout |
| ? | All shortcuts |

For a PDF (the backup when the venue has no internet), open the deck with
`?print-pdf` appended and print to PDF from Chrome.
