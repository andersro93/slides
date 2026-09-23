import type { RevealConfig } from "reveal.js";

/** /<code>/_deck.json, as written by apps/server/internal/web. */
export type DeckPayload = {
  code: string;
  title: string;
  description?: string;
  theme: string;
  css?: string[];
  reveal?: Record<string, unknown>;
  format: "markdown" | "html";
  content: string;
  dev?: boolean;
};

// Markdown decks: a line with only --- starts a new slide, -- a vertical
// one, and "Note:" begins the speaker notes.
export const MARKDOWN_SEPARATORS = {
  horizontal: String.raw`^\r?\n---\r?\n$`,
  vertical: String.raw`^\r?\n--\r?\n$`,
  notes: String.raw`^\s*[Nn]otes?:`,
};

const defaults: RevealConfig = {
  // The slide is in the URL: a reload (or a crashed browser) comes back
  // to the same place, and the speaker view stays in step.
  hash: true,
  controls: true,
  progress: true,
  slideNumber: "c/t",
  center: true,
  transition: "slide",
  // Content comes from our own repo.
  markdown: { smartypants: true },
};

/**
 * reveal.js options: our defaults, then the deck's own `reveal:` frontmatter.
 * Plugins are never overridable from a deck.
 */
export function buildConfig(
  deck: Pick<DeckPayload, "reveal">,
  plugins: NonNullable<RevealConfig["plugins"]>,
): RevealConfig {
  const { plugins: _ignored, ...fromDeck } = (deck.reveal ??
    {}) as RevealConfig;
  return { ...defaults, ...fromDeck, plugins };
}
