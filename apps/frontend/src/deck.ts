import Reveal from "reveal.js";
import Highlight from "reveal.js/plugin/highlight";
import Markdown from "reveal.js/plugin/markdown";
import Notes from "reveal.js/plugin/notes";
import Search from "reveal.js/plugin/search";
import Zoom from "reveal.js/plugin/zoom";
import "reveal.js/reveal.css";
import "reveal.js/plugin/highlight/monokai.css";
import "./deck.css";
import { codeFromPath } from "./lib/code";
import {
  buildConfig,
  type DeckPayload,
  MARKDOWN_SEPARATORS,
} from "./lib/deck-config";
import { setupPairing } from "./pairing";
import { loadTheme } from "./themes";

const code = codeFromPath(location.pathname);
const revealEl = document.querySelector<HTMLElement>(".reveal")!;
const slidesEl = document.querySelector<HTMLElement>(".reveal .slides")!;
const messageEl = document.querySelector<HTMLElement>(".deck-message")!;

function showMessage(text: string): void {
  messageEl.textContent = text;
  messageEl.hidden = false;
}

async function fetchDeck(): Promise<{ deck: DeckPayload; raw: string }> {
  const res = await fetch(`/${code}/_deck.json`, { cache: "no-cache" });
  const raw = await res.text();
  if (!res.ok) {
    let reason = res.statusText;
    try {
      reason = (JSON.parse(raw) as { error?: string }).error ?? reason;
    } catch {
      // Not JSON; keep the status text.
    }
    throw new Error(reason);
  }
  return { deck: JSON.parse(raw) as DeckPayload, raw };
}

function renderSlides(deck: DeckPayload): void {
  if (deck.format === "html") {
    // Our own content from the repo. Inline <script>s do not run this way;
    // decks that need behaviour belong in the frontend, not in a slide.
    slidesEl.innerHTML = deck.content;
    return;
  }
  const section = document.createElement("section");
  section.dataset.markdown = "";
  section.dataset.separator = MARKDOWN_SEPARATORS.horizontal;
  section.dataset.separatorVertical = MARKDOWN_SEPARATORS.vertical;
  section.dataset.separatorNotes = MARKDOWN_SEPARATORS.notes;
  // The markdown plugin reads the textContent of [data-template]; setting
  // it through the DOM means no escaping pitfalls for "</textarea>" etc.
  const template = document.createElement("textarea");
  template.dataset.template = "";
  template.textContent = deck.content;
  section.append(template);
  slidesEl.replaceChildren(section);
}

/** Resolves once the stylesheet has loaded (or failed), so layout is final. */
function addStylesheet(href: string): Promise<void> {
  return new Promise((resolve) => {
    const link = document.createElement("link");
    link.rel = "stylesheet";
    // Relative: /<code>/ is the page's base, so "style.css" is the deck's.
    link.href = href;
    link.onload = () => resolve();
    link.onerror = () => {
      console.warn(`deck stylesheet ${href} failed to load`);
      resolve();
    };
    document.head.append(link);
  });
}

// Dev mode (DECKS_DIR set on the server): reload when the source changes.
// The URL hash keeps the current slide across the reload.
function watchForChanges(initial: string): void {
  let last = initial;
  setInterval(async () => {
    try {
      const { raw } = await fetchDeck();
      if (raw !== last) {
        last = raw;
        location.reload();
      }
    } catch {
      // Mid-edit frontmatter errors show up on the next successful poll.
    }
  }, 1000);
}

async function main(): Promise<void> {
  // A deck URL without the trailing slash would break relative asset paths;
  // the server redirects, but be defensive about what the browser kept.
  if (!location.pathname.endsWith("/")) {
    location.replace(`/${code}/${location.search}${location.hash}`);
    return;
  }

  let deck: DeckPayload;
  let raw: string;
  try {
    ({ deck, raw } = await fetchDeck());
  } catch (err) {
    showMessage(`Could not load this presentation: ${(err as Error).message}`);
    return;
  }

  document.title = deck.title;
  if (deck.description) {
    const meta = document.createElement("meta");
    meta.name = "description";
    meta.content = deck.description;
    document.head.append(meta);
  }

  await Promise.all([
    loadTheme(deck.theme),
    ...(deck.css ?? []).map(addStylesheet),
  ]);
  renderSlides(deck);

  const reveal = new Reveal(
    revealEl,
    buildConfig(deck, [Markdown, Highlight, Notes, Zoom, Search]),
  );
  await reveal.initialize();

  // The speaker-notes window embeds this page as a "receiver"; only the
  // main window should own the phone remote.
  if (!new URLSearchParams(location.search).has("receiver")) {
    setupPairing(reveal, code, deck.title);
  }
  if (deck.dev) watchForChanges(raw);
}

void main();
