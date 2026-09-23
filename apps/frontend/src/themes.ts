// Every themes/<name>.{css,scss} becomes its own lazily-loaded chunk; a
// deck pays only for the theme it names. The server validates names against
// the same list (apps/server/internal/decks.Themes, kept in step by a Go
// test).
const themes = import.meta.glob("./themes/*.{css,scss}");

export async function loadTheme(name: string): Promise<void> {
  const load =
    themes[`./themes/${name}.css`] ??
    themes[`./themes/${name}.scss`] ??
    themes["./themes/ros.scss"];
  await load?.();
}
