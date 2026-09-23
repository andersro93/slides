import DOMPurify from "dompurify";

/**
 * Speaker notes arrive as HTML from the host side of a remote session, which
 * is not necessarily our own deck. Keep formatting (lists, emphasis, links,
 * code, images), drop everything that can run or restyle the page.
 */
export function sanitizeNotes(html: string): DocumentFragment {
  const fragment = DOMPurify.sanitize(html, {
    RETURN_DOM_FRAGMENT: true,
    FORBID_TAGS: ["style", "form", "input", "button", "textarea", "select"],
    FORBID_ATTR: ["style"],
  });
  for (const a of fragment.querySelectorAll("a")) {
    a.target = "_blank";
    a.rel = "noopener noreferrer";
  }
  return fragment;
}
