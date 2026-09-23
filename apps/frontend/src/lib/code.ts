// Presentation codes, mirroring apps/server/internal/decks: 3–64 lowercase
// letters, digits and inner hyphens.
const CODE = /^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$/;

export function isValidCode(code: string): boolean {
  return CODE.test(code);
}

/**
 * Turns whatever someone typed into a code: case and surrounding space do not
 * matter, inner spaces become hyphens, and a pasted link to a deck
 * ("https://slides.example.com/abc/#/3") is reduced to its code.
 */
export function normalizeCode(input: string): string {
  let s = input.trim();
  try {
    if (/^https?:\/\//i.test(s)) {
      s = new URL(s).pathname;
    }
  } catch {
    // Not a URL after all; treat it as typed.
  }
  s = s.replace(/^\/+/, "").split(/[/?#]/)[0] ?? "";
  return s.toLowerCase().replace(/\s+/g, "-");
}

/** The code segment of a /<code>/… path. */
export function codeFromPath(pathname: string): string {
  const first = pathname.split("/").filter(Boolean)[0] ?? "";
  try {
    return decodeURIComponent(first).toLowerCase();
  } catch {
    return first.toLowerCase();
  }
}
