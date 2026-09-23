import { createRequire } from "node:module";
import { dirname, resolve } from "node:path";
import { defineConfig } from "vite";

// reveal.js/css (the SCSS theme template) is shipped but not exported;
// resolve it through the package's main entry (dist/reveal.js).
const revealRoot = resolve(
  dirname(createRequire(import.meta.url).resolve("reveal.js")),
  "..",
);

// Three static pages, no framework. The Go server (apps/server) serves them:
//
//   index.html   at /              — the code field
//   deck.html    at /<code>/       — reveal.js; fetches /<code>/_deck.json
//   remote.html  at /<code>/_remote — the phone remote
//
// Everything else is content-hashed under /_app/ (served immutable). Not
// Vite's default /assets/: that name is what deck authors naturally use for
// their own images, and /_app/ can never collide with a deck code.
//
// Output goes to dist/app at the repo root; scripts/embed.sh copies it into
// the Go binary's go:embed directory, and dev mode (scripts/dev.sh) serves it
// from there directly via APP_DIR.
export default defineConfig({
  resolve: {
    alias: { "reveal-theme": resolve(revealRoot, "css/theme") },
  },
  build: {
    outDir: resolve(__dirname, "../../dist/app"),
    emptyOutDir: true,
    assetsDir: "_app",
    rollupOptions: {
      input: {
        index: resolve(__dirname, "index.html"),
        deck: resolve(__dirname, "deck.html"),
        remote: resolve(__dirname, "remote.html"),
      },
    },
  },
});
