// Package embedded carries the built frontend (app/, from apps/frontend),
// copied in by scripts/embed.sh before `go build`. In git the directory only
// holds a .gitkeep, so a bare `go build` still compiles — `serve` then
// refuses to start because the frontend is missing, rather than serving a
// blank page. Presentations are not embedded: they are mounted (DECKS_DIR).
package embedded

import (
	"embed"
	"io/fs"
)

//go:embed all:app
var files embed.FS

// App is the built frontend: index.html, deck.html, remote.html, _app/…
func App() fs.FS {
	s, err := fs.Sub(files, "app")
	if err != nil {
		// Only possible for an invalid path literal.
		panic(err)
	}
	return s
}
