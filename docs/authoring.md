# Writing and publishing presentations

- [Quick start](#quick-start): a first deck, live, in five steps
- [Publishing](#publishing): getting decks into the server's folder
- [Reference](#reference): folder rules, frontmatter, Markdown, HTML, styling
- [Presenting](#presenting): keys, phone remote, PDF backup
- [Troubleshooting](#troubleshooting)

The server has no upload or admin page. It serves whatever is in one
folder: the **presentations folder**, mounted into the container at `/decks`.
Put a deck in that folder and it is live within seconds; take it out and it
is gone.

## Quick start

**1. Copy the template** into your presentations folder, naming the
directory whatever you like (the name is only for you):

```sh
cp -R examples/_template ~/presentations/kubecon-2026
```

Starting from scratch works just as well; a deck is a directory with one
`index.md` in it.

**2. Give it a code and a title** at the top of
`kubecon-2026/index.md`:

```yaml
---
code: kubecon26
title: Scaling the thing
---
```

The **code** is what you type on the landing page and what ends up in the
URL: `https://slides.ros-nett.com/kubecon26/`.

**3. Write slides.** A line with only `---`, with a blank line above and
below, starts a new slide:

```markdown
# Scaling the thing

Anders, KubeCon 2026

---

## Why this matters

- One
- Two

Note:
Only I see this: speaker view (S) and the phone remote (R).
```

**4. Preview it** on your own machine. The easiest way needs only Docker:

```sh
docker run --rm -p 3000:3000 -v ~/presentations:/decks:ro -e DEV=1 ghcr.io/andersro93/slides
```

Open <http://localhost:3000/kubecon26/>. With `DEV=1` the open deck reloads
itself every time you save, and a deck with an error explains what is wrong
instead of just not being found. (The image is private: `docker login
ghcr.io` once, with a GitHub token that can read packages. With this repo
checked out, `DECKS_DIR=~/presentations mise run dev` does the same without
Docker.)

**5. Publish it** by getting the directory into the server's presentations
folder (see [Publishing](#publishing)). Then open
`https://slides.ros-nett.com`, type `kubecon26`, and present.

## Publishing

However the files get there, the rules are the same:

- The server checks the folder for changes every 5 seconds (`RELOAD_INTERVAL`).
  New, edited and deleted decks take effect without a restart.
- A deck with an error **keeps serving its last good version**, and the
  error goes to the server log. A brand-new deck with an error is simply
  not served until it is fixed. Nothing else is affected.
- Check a folder before it goes live:

  ```sh
  docker run --rm -v ~/presentations:/decks:ro ghcr.io/andersro93/slides validate
  ```

  It lists every deck and every problem, and exits non-zero on problems, so
  it also works as a CI step.

### Option A: a folder on the server

`deploy/compose.yaml` mounts `./presentations` (next to the compose file)
read-only at `/decks`. Copy decks there any way you like:

```sh
rsync -av --delete ~/presentations/ server:/srv/slides/presentations/
```

`--delete` also removes decks you deleted locally; leave it out to only
add or update. Syncthing, `scp`, or editing on the server work the same
way.

The container runs as an unprivileged user (uid 65532), so the files must
be world-readable. Normal permissions (`644` for files, `755` for
directories) are fine; `rsync -a` keeps whatever you have locally.

To skip the up-to-5-second wait after a sync:
`docker compose kill -s HUP slides`.

### Option B: a private git repository (git-sync)

Keep the presentations in their own private repository, and let the
`git-sync` service in `deploy/compose.yaml` pull it every minute. Pushing
to `main` then *is* publishing: the deck is live about a minute later.

- **The repository root is the presentations folder**: deck directories at
  the top level, not inside a `presentations/` subfolder.

  ```
  my-presentations/        ← the repo
    kubecon-2026/
      index.md
    board-review/
      index.md
    README.md              ← top-level files are ignored
  ```

- In `deploy/compose.yaml`: uncomment the `git-sync` service, the `decks`
  volume and `DECKS_DIR: /git/current`, and swap the `./presentations`
  mount for `decks:/git:ro`.
- git-sync needs a **read-only deploy key** for the repository
  (GitHub → repo → Settings → Deploy keys) saved as `./deploy-key`, and the
  host key in `./known_hosts` (`ssh-keyscan github.com > known_hosts`).
- Worth adding to that repository's CI, so a broken deck never reaches
  `main`:

  ```yaml
  - run: docker run --rm -v "$PWD:/decks:ro" ghcr.io/andersro93/slides validate
  ```

  (after a `docker/login-action` step for ghcr.io).

## Reference

### Folder rules

```
presentations/
  kubecon-2026/
    index.md          ← or index.html, never both
    style.css         ← optional, listed under css:
    images/
      diagram.png     ← referenced as images/diagram.png
  _drafts/            ← names starting with _ or . are ignored
```

- One directory per deck, directly inside the presentations folder
  (not nested deeper).
- Exactly one of `index.md` or `index.html` per deck.
- **Reference files relatively**: `images/diagram.png`, not
  `/images/diagram.png`. A deck lives at `/<code>/`, so relative paths
  resolve inside its own directory.
- Never served: the `index.md`/`index.html` source itself, anything whose
  name starts with `.`, and top-level names starting with `_`.
- Plain files only: symbolic links are refused, so the server can never
  serve anything from outside the folder.

### Frontmatter

Both formats start with a YAML block between `---` lines:

```yaml
---
code: kubecon26            # required
title: Scaling the thing   # required: the browser tab title
description: One line.     # optional
theme: ros                 # optional; ros (default) or any reveal.js theme:
                           #   black white league beige night serif simple
                           #   solarized moon dracula sky blood
                           #   black-contrast white-contrast
css: [style.css]           # optional; stylesheets inside this directory
private: true              # optional; requires a code of 12+ characters
draft: true                # optional; only served with DEV=1
reveal:                    # optional; passed straight to reveal.js
  transition: fade         #   https://revealjs.com/config/
  slideNumber: false
---
```

**Unknown keys are an error**, so a typo like `privat: true` never silently
does nothing.

**Codes.** 3–64 characters of `a-z`, `0-9` and hyphens (not first or last),
unique across the folder. On the landing page, case does not matter and
spaces become hyphens (`KubeCon 26` opens `kubecon-26`).
**Changing a code breaks every link and QR code you have shared.** Keep it
once it is out there.

**Public vs private.** Every deck can only be reached by its code; nothing
lists them, and wrong guesses are rate-limited. For a talk on stage, pick
something memorable (`kubecon26`). For a private meeting, pick something
nobody would guess (`mint-orbit-falcon-42`) and add `private: true`, which
enforces a length of at least 12.

**Drafts.** `draft: true` hides a deck from the live site while you work
on it, but it is still checked by `validate`, and it shows up in `DEV=1`
previews.

### Markdown (`index.md`)

```markdown
# Title slide

Note:
Speaker notes: everything after a line starting with "Note:" (or "Notes:")
until the next slide.

---

## A slide

- Blank lines around the separators are required
- `---` starts a new slide, `--` a vertical slide below this one

--

## A vertical slide

![Diagram](images/diagram.png)
```

- **Separators need a blank line above and below.** A consequence: a
  Markdown horizontal rule (`---`) always becomes a slide break.
- Per-slide settings go in a comment on the slide:
  `<!-- .slide: data-background-color="#f7f7f5" -->`, or
  `data-background-image="images/photo.jpg"`.
- Step through items one at a time:
  `- Point <!-- .element: class="fragment" -->`.
- Code blocks are highlighted; step through lines with
  ```` ```go [1-2|4-6] ````.
- Plain HTML is allowed in Markdown when you need layout.

### HTML (`index.html`)

After the frontmatter, write reveal.js markup directly:

```html
---
code: board-review-q3
title: Q3 review
---
<section>
  <h1>Q3 review</h1>
  <aside class="notes">Speaker notes.</aside>
</section>
<section>
  <section><h2>Nested sections…</h2></section>
  <section><h2>…are vertical slides</h2></section>
</section>
```

Everything from the [reveal.js docs](https://revealjs.com/markup/) works:
auto-animate, backgrounds, fragments. The one exception is `<script>`: it
does not run.

### Styling

Pick a `theme`, then adjust with a stylesheet in the deck's directory:

```yaml
css: [style.css]
```

```css
/* style.css: loaded after the theme, so plain rules win */
:root {
  --r-heading-color: #f97316;
  --r-link-color: #f97316;
  --r-main-font-size: 36px;
}

.reveal .big-number {
  font-size: 4em;
  font-weight: 800;
}
```

The `--r-…` variables cover colours, fonts and sizes for every theme; see
reveal.js's [theme docs](https://revealjs.com/themes/). The default `ros`
theme and all app fonts are bundled. Several stock reveal themes load their
fonts from Google Fonts, which some corporate networks block.

### Images, video and embeds

Local files are served from the deck's directory. Images, video and
iframes (YouTube, CodePen…) from other sites work as long as they use
`https://`. Anything that needs its own JavaScript does not.

## Presenting

| Key | |
| --- | --- |
| R | Control from your phone (shows a single-use QR code) |
| S | Speaker view (a separate window: allow pop-ups) |
| F | Fullscreen |
| O / Esc | Overview |
| B / . | Blackout |
| ? | All shortcuts |

**Phone remote.** Press R, scan the QR code with the phone's camera, and
the dialog closes by itself. The phone shows next/previous, blackout, the
current slide's notes, the next slide's title and a timer (it starts on
the first "Next"). The QR works once, so it does not matter who else sees
it on the projector. The phone stays paired through a deck reload or the
phone's screen locking. Press R again for a fresh code, e.g. to hand over
to another phone.

**PDF backup.** For venues without internet: open the deck with
`?print-pdf` at the end of the URL (`/kubecon26/?print-pdf`), then print to
PDF in Chrome with *Margins: none* and *Background graphics* on.

## Troubleshooting

**"No presentation with that code."**
- Check the code in the frontmatter, not the directory name.
- Is the deck marked `draft: true`?
- Did the deck ever load? A new deck with an error is not served at all.
  Run `validate` on the folder, or look in the server log:
  `docker compose logs slides | grep problems`.
- Is the deck directly inside the folder (not nested deeper), and is its
  name free of a leading `_` or `.`?

**My edit doesn't show up.**
- Give it 5 seconds (or about a minute with git-sync), then reload.
- If it still shows the old version, the edit has an error and the server
  is keeping the last good version. `validate` or the log says what.

**An image is missing.**
- Use a relative path (`images/a.png`, not `/images/a.png`) with the same
  upper/lower case as the file.
- Is it a symlink, or does its name start with `.`?
- `https://` for images from other sites.

**"Too many attempts."**
- Too many wrong codes from your network in 10 minutes. Wait, or use a
  different network.

**Slides split in the wrong places.**
- `---` and `--` on their own line, with blank lines around them, always
  split. Anything else never does.
