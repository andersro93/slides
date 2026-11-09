package decks

import (
	"io/fs"
	"path"
	"strings"
	"testing"
)

// Asset paths come straight from request URLs, so IsAssetPath is the line
// between a deck's files and everything else on disk.
func FuzzIsAssetPath(f *testing.F) {
	for _, s := range []string{
		"img/a.png", "style.css", "index.md", "index.html", ".env", "a/.git/config",
		"../secret", "a/../../b", "/etc/passwd", "a//b", "a/./b", "a/", ".", "",
		"img\\..\\a", "%2e%2e/a", "a\x00b",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, name string) {
		if !IsAssetPath(name) {
			return
		}
		if !fs.ValidPath(name) || path.Clean(name) != name || path.IsAbs(name) {
			t.Fatalf("IsAssetPath(%q): not a clean relative path", name)
		}
		if name == "index.md" || name == "index.html" {
			t.Fatalf("IsAssetPath(%q): the deck source must not be served", name)
		}
		for seg := range strings.SplitSeq(name, "/") {
			if seg == ".." || strings.HasPrefix(seg, ".") {
				t.Fatalf("IsAssetPath(%q): segment %q escapes or is hidden", name, seg)
			}
		}
	})
}

func FuzzSplitFrontmatter(f *testing.F) {
	for _, s := range []string{
		"---\ncode: a\n---\n# Hi\n",
		"---\r\ncode: a\r\n---\r\nbody",
		"\uFEFF---\ncode: a\n---",
		"---\n---\n",
		"---\ncode: a\n",
		"no frontmatter",
		"",
	} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		meta, body, err := splitFrontmatter(src)
		if err != nil {
			if meta != nil || body != "" {
				t.Fatalf("error %v with leftovers: meta=%q body=%q", err, meta, body)
			}
			return
		}
		// Both parts are cut from the normalised source, never invented.
		s := strings.ReplaceAll(string(src), "\r\n", "\n")
		if !strings.Contains(s, string(meta)) || !strings.HasSuffix(s, body) {
			t.Fatalf("parts not taken from the source: meta=%q body=%q", meta, body)
		}
		if strings.Contains(string(meta), "\n---\n") {
			t.Fatalf("meta runs past the closing ---: %q", meta)
		}
	})
}
