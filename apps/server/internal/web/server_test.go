package web

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/andersro93/slides/server/internal/decks"
	"github.com/andersro93/slides/server/internal/remote"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

var appFS = fstest.MapFS{
	"index.html":        file("<title>landing</title>"),
	"deck.html":         file("<title>deck</title>"),
	"remote.html":       file("<title>remote</title>"),
	"favicon.svg":       file("<svg/>"),
	"_app/deck-abc.js":  file("js"),
	"_app/theme-x.css":  file("css"),
	"stray.html":        file("never served directly"),
	"_app/nested/x.css": file("css"),
}

var decksFS = fstest.MapFS{
	"talk/index.md":       file("---\ncode: kubecon26\ntitle: A talk\n---\n# Hello\n"),
	"talk/img/a.png":      file("png-bytes"),
	"talk/.secret":        file("nope"),
	"talk/_hidden.txt":    file("underscore files are fine on disk but not routable"),
	"other/index.html":    file("---\ncode: other-deck\ntitle: Other\n---\n<section>x</section>"),
	"other/sub/index.md":  file("x"),
	"other/sub/notes.txt": file("notes"),
}

func newTestServer(t *testing.T, mutate func(*Options)) http.Handler {
	t.Helper()
	lib, err := decks.Load(decksFS, decks.Options{})
	if err != nil {
		t.Fatal(err)
	}
	o := Options{
		App:            appFS,
		Library:        func() *decks.Library { return lib },
		Hub:            remote.NewHub(remote.Options{}),
		ClientIPHeader: "CF-Connecting-IP",
	}
	if mutate != nil {
		mutate(&o)
	}
	s, err := New(o)
	if err != nil {
		t.Fatal(err)
	}
	return s.Handler()
}

func get(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	for i := 0; i+1 < len(hdr); i += 2 {
		req.Header.Set(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func body(rec *httptest.ResponseRecorder) string {
	b, _ := io.ReadAll(rec.Result().Body)
	return string(b)
}

func TestRoutes(t *testing.T) {
	h := newTestServer(t, nil)
	cases := []struct {
		path     string
		status   int
		contains string
		location string
	}{
		{"/", 200, "landing", ""},
		{"/healthz", 200, "ok", ""},
		{"/robots.txt", 200, "Disallow: /", ""},
		{"/favicon.svg", 200, "<svg/>", ""},
		{"/_app/deck-abc.js", 200, "js", ""},
		{"/_app/nested/x.css", 200, "css", ""},
		{"/_app/missing.js", 404, "", ""},
		{"/_app/nested", 404, "", ""},
		{"/stray.html", 404, "", ""},
		{"/index.html", 404, "", ""},

		{"/kubecon26", 301, "", "/kubecon26/"},
		{"/KubeCon26", 301, "", "/kubecon26/"},
		{"/kubecon26/", 200, "<title>deck", ""},
		{"/KUBECON26/_deck.json", 301, "", "/kubecon26/_deck.json"},
		{"/kubecon26/_remote", 200, "<title>remote", ""},
		{"/kubecon26/img/a.png", 200, "png-bytes", ""},
		{"/other-deck/sub/notes.txt", 200, "notes", ""},

		// Never exposed: the source, dot-files, "_" names, directories,
		// traversal, and anything of another deck.
		{"/kubecon26/index.md", 404, "", ""},
		{"/kubecon26/.secret", 404, "", ""},
		{"/kubecon26/_hidden.txt", 404, "", ""},
		{"/kubecon26/img", 404, "", ""},
		{"/kubecon26/img/", 404, "", ""},
		{"/kubecon26/..%2fother%2findex.html", 404, "", ""},
		{"/kubecon26/sub/notes.txt", 404, "", ""},

		{"/nope", 404, "landing", ""},
		{"/nope/", 404, "landing", ""},
		{"/nope/img/a.png", 404, "landing", ""},
		{"/nope/_deck.json", 404, `"error"`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.path, func(t *testing.T) {
			// A fresh client per case so the miss limiter never interferes.
			rec := get(h, tc.path, "CF-Connecting-IP", "198.51.100."+tc.path)
			if rec.Code != tc.status {
				t.Fatalf("status = %d, want %d (%s)", rec.Code, tc.status, body(rec))
			}
			if tc.contains != "" && !strings.Contains(body(rec), tc.contains) {
				t.Errorf("body %q does not contain %q", body(rec), tc.contains)
			}
			if loc := rec.Header().Get("Location"); loc != tc.location {
				t.Errorf("Location = %q, want %q", loc, tc.location)
			}
		})
	}
}

func TestDeckJSON(t *testing.T) {
	h := newTestServer(t, nil)
	rec := get(h, "/kubecon26/_deck.json")
	if rec.Code != 200 {
		t.Fatal(rec.Code)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(body(rec)), &got); err != nil {
		t.Fatal(err)
	}
	if got["code"] != "kubecon26" || got["format"] != "markdown" || got["theme"] != "ros" || got["content"] != "# Hello\n" {
		t.Errorf("payload = %v", got)
	}
	for _, internal := range []string{"Dir", "Files", "Private", "Draft", "dev"} {
		if _, ok := got[internal]; ok {
			t.Errorf("payload leaks %q", internal)
		}
	}

	dev := newTestServer(t, func(o *Options) { o.Dev = true })
	if !strings.Contains(body(get(dev, "/kubecon26/_deck.json")), `"dev":true`) {
		t.Error("dev flag missing in dev mode")
	}
}

func TestHeaders(t *testing.T) {
	h := newTestServer(t, nil)
	for _, path := range []string{"/", "/kubecon26/", "/nope", "/_app/deck-abc.js"} {
		rec := get(h, path)
		for _, name := range []string{"Content-Security-Policy", "Referrer-Policy", "X-Robots-Tag", "X-Content-Type-Options"} {
			if rec.Header().Get(name) == "" {
				t.Errorf("%s: missing %s", path, name)
			}
		}
	}
	if csp := get(h, "/kubecon26/_remote").Header().Get("Content-Security-Policy"); !strings.Contains(csp, "script-src 'self';") {
		t.Errorf("remote CSP = %q, want no inline scripts", csp)
	}
	if cc := get(h, "/_app/deck-abc.js").Header().Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Errorf("_app Cache-Control = %q", cc)
	}
	if cc := get(h, "/kubecon26/").Header().Get("Cache-Control"); cc != "no-cache" {
		t.Errorf("shell Cache-Control = %q", cc)
	}
	if cc := get(h, "/kubecon26/img/a.png").Header().Get("Cache-Control"); !strings.HasPrefix(cc, "private") {
		t.Errorf("asset Cache-Control = %q", cc)
	}
}

func TestMissLimiter(t *testing.T) {
	h := newTestServer(t, func(o *Options) { o.MaxMisses = 3; o.MissWindow = time.Hour })
	attacker := []string{"CF-Connecting-IP", "203.0.113.9"}

	for i := range 3 {
		if rec := get(h, "/guess"+string(rune('a'+i)), attacker...); rec.Code != 404 {
			t.Fatalf("miss %d: status %d", i, rec.Code)
		}
	}
	// Blocked now — even for a valid code, so the limit is not an oracle.
	if rec := get(h, "/kubecon26/", attacker...); rec.Code != 429 {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if rec := get(h, "/kubecon26/_deck.json", attacker...); rec.Code != 429 || !strings.Contains(body(rec), "error") {
		t.Fatalf("json status = %d", rec.Code)
	}
	// Someone else is unaffected, and so are non-deck routes.
	if rec := get(h, "/kubecon26/", "CF-Connecting-IP", "192.0.2.1"); rec.Code != 200 {
		t.Fatalf("other client status = %d", rec.Code)
	}
	if rec := get(h, "/", attacker...); rec.Code != 200 {
		t.Fatalf("landing status = %d", rec.Code)
	}
}

func TestLimiterWindow(t *testing.T) {
	now := time.Now()
	l := newMissLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	l.Miss("a")
	l.Miss("a")
	if !l.Blocked("a") || l.Blocked("b") {
		t.Fatal("expected a blocked, b not")
	}
	now = now.Add(2 * time.Minute)
	if l.Blocked("a") {
		t.Fatal("window should have expired")
	}
	l.Miss("b") // prunes expired entries
	if _, ok := l.clients["a"]; ok {
		t.Fatal("expired client not pruned")
	}
}

func TestClientIPWithoutHeader(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req.RemoteAddr = "192.0.2.7:5555"
	req.Header.Set("CF-Connecting-IP", "10.0.0.1")
	if got := s.clientIP(req); got != "192.0.2.7" {
		t.Fatalf("clientIP = %q; the header must be ignored unless configured", got)
	}
}

func TestNewRequiresBuiltFrontend(t *testing.T) {
	_, err := New(Options{App: fstest.MapFS{"index.html": file("")}})
	if err == nil || !strings.Contains(err.Error(), "deck.html") {
		t.Fatalf("err = %v", err)
	}
}

func TestDevShowsProblemsOnMiss(t *testing.T) {
	problem := errors.New("bad/index.md: must start with a --- frontmatter block")
	h := newTestServer(t, func(o *Options) { o.Problems = func() error { return problem } })
	rec := get(h, "/anything/_deck.json")
	if rec.Code != 500 || !strings.Contains(body(rec), "frontmatter") {
		t.Fatalf("status %d body %s", rec.Code, body(rec))
	}
	// Valid decks are unaffected.
	if rec := get(h, "/kubecon26/_deck.json"); rec.Code != 200 {
		t.Fatalf("valid deck status %d", rec.Code)
	}
}

func TestClientKey(t *testing.T) {
	for raw, want := range map[string]string{
		"192.0.2.7":                "192.0.2.7",
		"::ffff:192.0.2.7":         "192.0.2.7",
		"2001:db8:1:2:aaaa::1":     "2001:db8:1:2::/64",
		"2001:db8:1:2:ffff:ffff::": "2001:db8:1:2::/64",
		"2001:db8:1:3::1":          "2001:db8:1:3::/64",
		"not-an-ip":                "not-an-ip",
	} {
		if got := clientKey(raw); got != want {
			t.Errorf("clientKey(%q) = %q, want %q", raw, got, want)
		}
	}
}

func TestLimiterIsBounded(t *testing.T) {
	l := newMissLimiter(1, time.Hour)
	l.maxClients = 2
	l.Miss("a")
	l.Miss("b")
	l.Miss("c") // untracked: the map is full of live windows
	if len(l.clients) != 2 || l.Blocked("c") || !l.Blocked("a") {
		t.Fatalf("clients = %v", l.clients)
	}
}
