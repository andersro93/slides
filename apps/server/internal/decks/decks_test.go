package decks_test

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/andersro93/slides/server/internal/decks"
)

func file(s string) *fstest.MapFile { return &fstest.MapFile{Data: []byte(s)} }

func TestLoadMarkdownAndHTML(t *testing.T) {
	fsys := fstest.MapFS{
		"talk/index.md":     file("---\ncode: kubecon26\ntitle: A talk\nreveal:\n  transition: fade\n---\n# Hello\n\n---\n\n# World\n"),
		"talk/img/a.png":    file("png"),
		"html/index.html":   file("---\r\ncode: html-deck\r\ntitle: HTML\r\ntheme: white\r\ncss: [style.css]\r\n---\r\n<section>Hi</section>\r\n"),
		"html/style.css":    file("h1{}"),
		"_drafts/index.md":  file("not a deck"),
		".hidden/index.md":  file("not a deck"),
		"README.md":         file("top-level files are ignored"),
		"draft/index.md":    file("---\ncode: wip-deck\ntitle: WIP\ndraft: true\n---\n# WIP\n"),
		"secret/index.md":   file("---\ncode: mint-orbit-falcon\ntitle: Secret\nprivate: true\n---\n# Shh\n"),
		"secret/notes.txt":  file("x"),
		"another/index.md":  file("---\ncode: abc\ntitle: Short public code is fine\n---\nx"),
		"another/.env":      file("SECRET=1"),
		"another/sub/b.svg": file("<svg/>"),
	}

	lib, err := decks.Load(fsys, decks.Options{})
	if err != nil {
		t.Fatal(err)
	}

	var codes []string
	for _, d := range lib.All() {
		codes = append(codes, d.Code)
	}
	if want := []string{"abc", "html-deck", "kubecon26", "mint-orbit-falcon"}; !slices.Equal(codes, want) {
		t.Fatalf("codes = %v, want %v (drafts excluded)", codes, want)
	}

	md, _ := lib.Lookup("kubecon26")
	if md.Format != decks.Markdown || md.Theme != decks.DefaultTheme || md.Dir != "talk" {
		t.Errorf("markdown deck = %+v", md)
	}
	if !strings.HasPrefix(md.Content, "# Hello") {
		t.Errorf("frontmatter not stripped: %q", md.Content)
	}
	if md.Reveal["transition"] != "fade" {
		t.Errorf("reveal options = %v", md.Reveal)
	}

	html, _ := lib.Lookup("html-deck")
	if html.Format != decks.HTML || html.Theme != "white" || html.Content != "<section>Hi</section>\n" {
		t.Errorf("html deck = %+v", html)
	}

	withDrafts, err := decks.Load(fsys, decks.Options{IncludeDrafts: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := withDrafts.Lookup("wip-deck"); !ok {
		t.Error("draft missing with IncludeDrafts")
	}
}

func TestLoadRejects(t *testing.T) {
	cases := map[string]struct {
		files fstest.MapFS
		want  string
	}{
		"no source":        {fstest.MapFS{"a/x.md": file("")}, "no index.md or index.html"},
		"both sources":     {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\n---\nx"), "a/index.html": file("x")}, "both index.md and index.html"},
		"no frontmatter":   {fstest.MapFS{"a/index.md": file("# Hi")}, "must start with a ---"},
		"unclosed":         {fstest.MapFS{"a/index.md": file("---\ncode: abc\n# Hi")}, "not closed"},
		"missing code":     {fstest.MapFS{"a/index.md": file("---\ntitle: t\n---\nx")}, "code is required"},
		"missing title":    {fstest.MapFS{"a/index.md": file("---\ncode: abc\n---\nx")}, "title is required"},
		"uppercase code":   {fstest.MapFS{"a/index.md": file("---\ncode: KubeCon\ntitle: t\n---\nx")}, "lowercase"},
		"underscore code":  {fstest.MapFS{"a/index.md": file("---\ncode: _app\ntitle: t\n---\nx")}, "lowercase"},
		"reserved code":    {fstest.MapFS{"a/index.md": file("---\ncode: healthz\ntitle: t\n---\nx")}, "reserved"},
		"short private":    {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\nprivate: true\n---\nx")}, "at least 12"},
		"unknown field":    {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\nprivat: true\n---\nx")}, "privat"},
		"unknown theme":    {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\ntheme: nope\n---\nx")}, `theme "nope"`},
		"missing css":      {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\ncss: [x.css]\n---\nx")}, `css "x.css"`},
		"escaping css":     {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\ncss: [../b/x.css]\n---\nx"), "b/x.css": file("")}, `css "../b/x.css"`},
		"empty body":       {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\n---\n\n")}, "no slides"},
		"duplicate code":   {fstest.MapFS{"a/index.md": file("---\ncode: abc\ntitle: t\n---\nx"), "b/index.md": file("---\ncode: abc\ntitle: t\n---\nx")}, "already used by a"},
		"errors collected": {fstest.MapFS{"a/index.md": file("# x"), "b/index.md": file("# y")}, "b: "},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := decks.Load(tc.files, decks.Options{})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

func TestIsAssetPath(t *testing.T) {
	for name, want := range map[string]bool{
		"img/a.png":    true,
		"style.css":    true,
		"index.md":     false,
		"index.html":   false,
		"sub/index.md": true,
		".env":         false,
		"a/.git/x":     false,
		"../x":         false,
		"a//b":         false,
		"/abs":         false,
		"":             false,
		".":            false,
	} {
		if got := decks.IsAssetPath(name); got != want {
			t.Errorf("IsAssetPath(%q) = %v, want %v", name, got, want)
		}
	}
}

// The example decks (used by `mise run dev`) must always load.
func TestExamples(t *testing.T) {
	lib, err := decks.Load(os.DirFS("../../../../examples"), decks.Options{IncludeDrafts: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(lib.All()) == 0 {
		t.Fatal("examples/ has no decks")
	}
}

// decks.Themes must match the theme files the frontend actually ships.
func TestThemesMatchFrontend(t *testing.T) {
	paths, err := filepath.Glob("../../../frontend/src/themes/*.*css")
	if err != nil {
		t.Fatal(err)
	}
	var shipped []string
	for _, p := range paths {
		shipped = append(shipped, strings.TrimSuffix(filepath.Base(p), filepath.Ext(p)))
	}
	want := slices.Clone(decks.Themes)
	slices.Sort(want)
	slices.Sort(shipped)
	if !slices.Equal(shipped, want) {
		t.Fatalf("frontend themes %v != decks.Themes %v", shipped, want)
	}
}
