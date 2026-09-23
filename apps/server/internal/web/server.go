// Package web is the HTTP surface:
//
//	GET /                      landing page (one text field)
//	GET /healthz               liveness
//	GET /robots.txt            disallow everything
//	GET /_app/…                hashed frontend assets (immutable)
//	GET /<file.ext>            frontend root files (favicon…)
//	GET /<code>                → 301 /<code>/
//	GET /<code>/               the deck (reveal.js shell; it fetches _deck.json)
//	GET /<code>/_deck.json     the deck's metadata and slides
//	GET /<code>/_remote        the phone remote
//	GET /<code>/_ws            remote-control WebSocket (internal/remote)
//	GET /<code>/<asset path>   files from the deck's directory
//
// Unknown codes get the landing page with a 404, which shows "no such
// presentation". Misses are rate-limited per client. There is deliberately no
// listing of any kind: knowing the code is the only way in.
package web

import (
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"time"

	"github.com/andersro93/slides/server/internal/decks"
	"github.com/andersro93/slides/server/internal/remote"
)

type Options struct {
	// App is the built frontend (index.html, deck.html, remote.html, _app/).
	App fs.FS
	// Library returns the current decks (decks.Source.Library).
	Library func() *decks.Library
	// Problems, when set, reports decks that failed to load. Only wired in
	// dev mode, where an unknown code then shows the author what is wrong
	// instead of a bare 404.
	Problems func() error
	Hub      *remote.Hub
	// ClientIPHeader, when set, is trusted for the client's address (see
	// config.Config.ClientIPHeader).
	ClientIPHeader string
	// Dev marks deck payloads so the page reloads itself on source changes.
	Dev    bool
	Logger *slog.Logger

	// MaxMisses per MissWindow before a client is blocked (default 30 / 10 min).
	MaxMisses  int
	MissWindow time.Duration
}

type Server struct {
	app      fs.FS
	library  func() *decks.Library
	problems func() error
	hub      *remote.Hub
	ipHeader string
	dev      bool
	log      *slog.Logger
	limiter  *missLimiter
}

var requiredPages = []string{"index.html", "deck.html", "remote.html"}

func New(o Options) (*Server, error) {
	for _, p := range requiredPages {
		if _, err := fs.Stat(o.App, p); err != nil {
			return nil, errors.New("frontend is not built (missing " + p + "): run scripts/embed.sh, or set APP_DIR")
		}
	}
	s := &Server{
		app:      o.App,
		library:  o.Library,
		problems: o.Problems,
		hub:      o.Hub,
		ipHeader: o.ClientIPHeader,
		dev:      o.Dev,
		log:      o.Logger,
	}
	if s.log == nil {
		s.log = slog.Default()
	}
	maxMisses, window := o.MaxMisses, o.MissWindow
	if maxMisses == 0 {
		maxMisses = 30
	}
	if window == 0 {
		window = 10 * time.Minute
	}
	s.limiter = newMissLimiter(maxMisses, window)
	return s, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", s.landing)
	mux.HandleFunc("GET /healthz", s.healthz)
	mux.HandleFunc("GET /robots.txt", s.robots)
	mux.HandleFunc("GET /_app/{path...}", s.appAsset)
	mux.HandleFunc("GET /{name}", s.root)
	mux.HandleFunc("GET /{code}/{rest...}", s.deck)
	return securityHeaders(mux)
}

// Content-Security-Policy notes:
//   - script-src needs 'unsafe-inline': reveal's speaker-notes window is an
//     about:blank popup (inheriting this policy) that the notes plugin fills
//     with document.write, inline scripts included. All content is ours.
//   - Google Fonts are allowed because several stock reveal themes @import
//     them; the default "ros" theme bundles its fonts and needs neither.
//   - img/media/frame accept any https source so decks can embed remote
//     images, videos and iframes (YouTube, CodePen…).
const csp = "default-src 'self'; " +
	"script-src 'self' 'unsafe-inline'; " +
	"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com; " +
	"font-src 'self' data: https://fonts.gstatic.com; " +
	"img-src 'self' data: blob: https:; " +
	"media-src 'self' blob: https:; " +
	"connect-src 'self'; " +
	"frame-src 'self' https:; " +
	"frame-ancestors 'self'; " +
	"base-uri 'self'; form-action 'self'; object-src 'none'"

// The phone remote renders speaker notes relayed from whoever runs the host
// side of its session — anyone who knows a deck's code can be that. The page
// sanitizes them, and this policy is the second line: no inline script at
// all (it needs none; only the deck's speaker-notes popup does).
const remoteCSP = "default-src 'self'; " +
	"script-src 'self'; " +
	"style-src 'self' 'unsafe-inline'; " +
	"font-src 'self' data:; " +
	"img-src 'self' data: https:; " +
	"connect-src 'self'; " +
	"frame-src 'none'; frame-ancestors 'none'; " +
	"base-uri 'none'; form-action 'none'; object-src 'none'"

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("Content-Security-Policy", csp)
		h.Set("X-Content-Type-Options", "nosniff")
		// The code is in every URL; never leak it to sites a deck links to.
		h.Set("Referrer-Policy", "no-referrer")
		h.Set("X-Robots-Tag", "noindex, nofollow, noarchive")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		next.ServeHTTP(w, r)
	})
}

func (s *Server) healthz(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write([]byte("ok\n"))
}

func (s *Server) robots(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte("User-agent: *\nDisallow: /\n"))
}

func (s *Server) landing(w http.ResponseWriter, r *http.Request) {
	s.page(w, "index.html", http.StatusOK)
}

func (s *Server) appAsset(w http.ResponseWriter, r *http.Request) {
	name := "_app/" + r.PathValue("path")
	if !isRegular(s.app, name) {
		http.NotFound(w, r)
		return
	}
	// Vite content-hashes every file under _app/.
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	http.ServeFileFS(w, r, s.app, name)
}

// root is /{name}: a frontend root file when it has an extension, otherwise a
// code missing its trailing slash.
func (s *Server) root(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if strings.Contains(name, ".") {
		if strings.HasSuffix(name, ".html") || !isRegular(s.app, name) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=86400")
		http.ServeFileFS(w, r, s.app, name)
		return
	}
	if _, ok := s.lookup(w, r, name, false); ok {
		http.Redirect(w, r, "/"+strings.ToLower(name)+"/", http.StatusMovedPermanently)
	}
}

func (s *Server) deck(w http.ResponseWriter, r *http.Request) {
	code, rest := r.PathValue("code"), r.PathValue("rest")
	deck, ok := s.lookup(w, r, code, rest == "_deck.json")
	if !ok {
		return
	}
	if lower := strings.ToLower(code); lower != code {
		target := "/" + lower + "/" + rest
		if r.URL.RawQuery != "" {
			target += "?" + r.URL.RawQuery
		}
		http.Redirect(w, r, target, http.StatusMovedPermanently)
		return
	}

	switch rest {
	case "":
		s.page(w, "deck.html", http.StatusOK)
	case "_deck.json":
		s.deckJSON(w, deck)
	case "_remote":
		w.Header().Set("Content-Security-Policy", remoteCSP)
		s.page(w, "remote.html", http.StatusOK)
	case "_ws":
		client := s.clientIP(r)
		s.hub.Serve(w, r, deck.Code, client, func() { s.limiter.Miss(client) })
	default:
		if strings.HasPrefix(rest, "_") || !decks.IsAssetPath(rest) || !decks.IsPlainFile(deck.Files, rest) {
			http.NotFound(w, r)
			return
		}
		// "private": the code is in the URL, but keep shared caches
		// (Cloudflare's edge included) out of it anyway.
		w.Header().Set("Cache-Control", "private, max-age=300")
		http.ServeFileFS(w, r, deck.Files, rest)
	}
}

// lookup resolves a code, answering the request itself (429, 404, 500) when
// it cannot.
func (s *Server) lookup(w http.ResponseWriter, r *http.Request, code string, asJSON bool) (*decks.Deck, bool) {
	client := s.clientIP(r)
	if s.limiter.Blocked(client) {
		w.Header().Set("Retry-After", "600")
		s.fail(w, http.StatusTooManyRequests, "too many attempts, try again later", asJSON)
		return nil, false
	}
	deck, ok := s.library().Lookup(strings.ToLower(code))
	if !ok {
		s.limiter.Miss(client)
		if s.problems != nil {
			if err := s.problems(); err != nil {
				s.fail(w, http.StatusInternalServerError, "presentations have problems:\n"+err.Error(), asJSON)
				return nil, false
			}
		}
		if asJSON {
			s.fail(w, http.StatusNotFound, "no presentation with that code", true)
		} else {
			// The landing page reads the path and says "no such code".
			s.page(w, "index.html", http.StatusNotFound)
		}
		return nil, false
	}
	return deck, true
}

type deckPayload struct {
	*decks.Deck
	Dev bool `json:"dev,omitempty"`
}

func (s *Server) deckJSON(w http.ResponseWriter, d *decks.Deck) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-cache, private")
	if err := json.NewEncoder(w).Encode(deckPayload{Deck: d, Dev: s.dev}); err != nil {
		s.log.Error("encoding deck", "code", d.Code, "err", err)
	}
}

func (s *Server) page(w http.ResponseWriter, name string, status int) {
	body, err := fs.ReadFile(s.app, name)
	if err != nil {
		s.log.Error("reading page", "page", name, "err", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	// The shells reference hashed assets; they must be revalidated so a
	// deploy is picked up immediately.
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(status)
	_, _ = w.Write(body)
}

func (s *Server) fail(w http.ResponseWriter, status int, msg string, asJSON bool) {
	w.Header().Set("Cache-Control", "no-store")
	if asJSON {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": msg})
		return
	}
	http.Error(w, msg, status)
}

// clientIP is the key a client is limited by: its IPv4 address, or its IPv6
// /64 — one subscriber usually has a whole /64, and keying on single v6
// addresses would give every guesser 2^64 fresh identities.
func (s *Server) clientIP(r *http.Request) string {
	raw := ""
	if s.ipHeader != "" {
		raw = strings.TrimSpace(r.Header.Get(s.ipHeader))
	}
	if raw == "" {
		host, _, err := net.SplitHostPort(r.RemoteAddr)
		if err != nil {
			host = r.RemoteAddr
		}
		raw = host
	}
	return clientKey(raw)
}

func clientKey(raw string) string {
	addr, err := netip.ParseAddr(raw)
	if err != nil {
		return raw
	}
	addr = addr.Unmap()
	if addr.Is4() {
		return addr.String()
	}
	prefix, err := addr.Prefix(64)
	if err != nil {
		return addr.String()
	}
	return prefix.String()
}

func isRegular(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) {
		return false
	}
	info, err := fs.Stat(fsys, name)
	return err == nil && info.Mode().IsRegular()
}
