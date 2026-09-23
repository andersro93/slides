package decks_test

import (
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/andersro93/slides/server/internal/decks"
)

func deckFile(code, title, body string) string {
	return "---\ncode: " + code + "\ntitle: " + title + "\n---\n" + body
}

func TestReloadKeepsLastGoodVersion(t *testing.T) {
	fsys := fstest.MapFS{
		"a/index.md": file(deckFile("alpha", "Alpha v1", "# a")),
		"b/index.md": file(deckFile("beta", "Beta v1", "# b")),
	}
	lib, err := decks.Reload(nil, fsys, decks.Options{})
	if err != nil {
		t.Fatal(err)
	}

	// alpha breaks mid-edit, beta is updated, gamma is new and broken.
	fsys["a/index.md"] = file("---\ncode: alpha\n# forgot to close")
	fsys["b/index.md"] = file(deckFile("beta", "Beta v2", "# b"))
	fsys["c/index.md"] = file("no frontmatter")

	lib2, err := decks.Reload(lib, fsys, decks.Options{})
	if err == nil || !strings.Contains(err.Error(), "a: ") || !strings.Contains(err.Error(), "c: ") {
		t.Fatalf("err = %v", err)
	}
	if d, ok := lib2.Lookup("alpha"); !ok || d.Title != "Alpha v1" {
		t.Fatalf("alpha = %+v, want last good v1", d)
	}
	if d, _ := lib2.Lookup("beta"); d.Title != "Beta v2" {
		t.Fatalf("beta = %+v, want v2", d)
	}
	if len(lib2.All()) != 2 {
		t.Fatalf("decks = %d", len(lib2.All()))
	}

	// A deleted directory is gone, even if it was good before.
	delete(fsys, "b/index.md")
	lib3, _ := decks.Reload(lib2, fsys, decks.Options{})
	if _, ok := lib3.Lookup("beta"); ok {
		t.Fatal("deleted deck still served")
	}
}

func TestReloadUnreadableKeepsEverything(t *testing.T) {
	lib, _ := decks.Reload(nil, fstest.MapFS{"a/index.md": file(deckFile("alpha", "A", "x"))}, decks.Options{})
	got, err := decks.Reload(lib, os.DirFS(filepath.Join(t.TempDir(), "missing")), decks.Options{})
	if err == nil || got != lib {
		t.Fatalf("got %v, %v; want previous library and an error", got, err)
	}
}

func TestSourceRefreshesOnChange(t *testing.T) {
	dir := t.TempDir()
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("talk/index.md", deckFile("talk", "One", "# 1"))

	src, err := decks.NewSource(os.DirFS(dir), decks.Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	if d, ok := src.Library().Lookup("talk"); !ok || d.Title != "One" {
		t.Fatalf("initial = %+v", d)
	}
	if src.Refresh() {
		t.Fatal("refresh without changes reloaded")
	}

	write("talk/index.md", deckFile("talk", "Two", "# 2 (longer)"))
	write("new/index.md", deckFile("new-deck", "New", "# n"))
	if !src.Refresh() {
		t.Fatal("change not detected")
	}
	if d, _ := src.Library().Lookup("talk"); d.Title != "Two" {
		t.Fatalf("talk = %+v", d)
	}
	if _, ok := src.Library().Lookup("new-deck"); !ok {
		t.Fatal("new deck missing")
	}

	write("talk/index.md", "broken")
	src.Refresh()
	if src.Problems() == nil {
		t.Fatal("problems not reported")
	}
	if d, _ := src.Library().Lookup("talk"); d.Title != "Two" {
		t.Fatalf("broken deck not kept at last good: %+v", d)
	}
}

func TestNewSourceRequiresDirectory(t *testing.T) {
	if _, err := decks.NewSource(os.DirFS(filepath.Join(t.TempDir(), "nope")), decks.Options{}, nil); err == nil {
		t.Fatal("expected an error for a missing directory")
	}
}

func TestSourceWatchPoke(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "a"), 0o755); err != nil {
		t.Fatal(err)
	}
	src, err := decks.NewSource(os.DirFS(dir), decks.Options{}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	poke := make(chan struct{})
	ctx := t.Context()
	go src.Watch(ctx, time.Hour, poke)

	if err := os.WriteFile(filepath.Join(dir, "a/index.md"), []byte(deckFile("poked", "P", "x")), 0o644); err != nil {
		t.Fatal(err)
	}
	poke <- struct{}{}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, ok := src.Library().Lookup("poked"); ok {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("poke did not trigger a reload")
}

// A mounted folder must not become a window onto the rest of the host.
func TestSymlinksAreRefused(t *testing.T) {
	dir := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outside, "index.md"), []byte(deckFile("linked", "L", "x")), 0o644); err != nil {
		t.Fatal(err)
	}
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	must(os.MkdirAll(filepath.Join(dir, "talk"), 0o755))
	must(os.WriteFile(filepath.Join(dir, "talk/index.md"), []byte(deckFile("talk", "T", "x")), 0o644))
	must(os.WriteFile(filepath.Join(dir, "talk/ok.txt"), []byte("ok"), 0o644))
	must(os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(dir, "talk/secret.txt")))
	must(os.Symlink(outside, filepath.Join(dir, "talk/sub")))
	must(os.MkdirAll(filepath.Join(dir, "linked"), 0o755))
	must(os.Symlink(filepath.Join(outside, "index.md"), filepath.Join(dir, "linked/index.md")))

	fsys := os.DirFS(dir)
	for name, want := range map[string]bool{
		"talk/ok.txt":         true,
		"talk/secret.txt":     false,
		"talk/sub/secret.txt": false,
		"talk":                false, // a directory
		"talk/missing":        false,
	} {
		if got := decks.IsPlainFile(fsys, name); got != want {
			t.Errorf("IsPlainFile(%q) = %v, want %v", name, got, want)
		}
	}

	lib, err := decks.Reload(nil, fsys, decks.Options{})
	if err == nil || !strings.Contains(err.Error(), "linked: no index.md") {
		t.Fatalf("err = %v, want the symlinked source refused", err)
	}
	if _, ok := lib.Lookup("linked"); ok {
		t.Fatal("deck with symlinked source was loaded")
	}
}
