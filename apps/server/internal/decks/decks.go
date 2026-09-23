// Package decks reads the presentations tree into a code → deck lookup.
//
// Layout: one directory per presentation, holding exactly one of index.md
// (reveal.js Markdown) or index.html (reveal.js <section> markup), each
// starting with a YAML frontmatter block, plus any assets it references
// relatively. Directories whose name starts with "_" or "." are ignored, so
// _drafts/ or _template/ can live alongside real decks.
//
// Two ways to load: Load is strict (any invalid deck fails the whole load —
// `slides validate`, CI), Reload is lenient (a deck that fails keeps its
// last good version and everything else still updates — the running server,
// so one typo never takes a deck offline mid-meeting).
//
// Symlinks are refused for deck sources and assets: the folder is mounted
// from outside, and a link must not turn into a way to serve files from
// elsewhere on the host.
package decks

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"go.yaml.in/yaml/v3"
)

type Format string

const (
	Markdown Format = "markdown"
	HTML     Format = "html"
)

// Themes the frontend ships (apps/frontend/src/themes/<name>.css). A test
// keeps this list and that directory in step.
var Themes = []string{
	"ros",
	"beige", "black", "black-contrast", "blood", "dracula", "league", "moon",
	"night", "serif", "simple", "sky", "solarized", "white", "white-contrast",
}

const DefaultTheme = "ros"

// PrivateMinLength is the shortest code a `private: true` deck may have. The
// code is the only thing standing between a stranger and the deck, so a
// private one has to be long enough not to be guessed (the server also
// rate-limits misses).
const PrivateMinLength = 12

// Codes are URL path segments: lowercase letters, digits and inner hyphens,
// 3–64 characters. No "_" (reserved for the server's own routes such as
// /_app/ and /<code>/_ws) and no "." (reserved for root files like
// /robots.txt).
var codePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,62}[a-z0-9]$`)

// Codes that collide with fixed top-level routes.
var reservedCodes = []string{"healthz"}

type Deck struct {
	Code        string         `json:"code"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	Theme       string         `json:"theme"`
	CSS         []string       `json:"css,omitempty"`
	Reveal      map[string]any `json:"reveal,omitempty"`
	Format      Format         `json:"format"`
	Content     string         `json:"content"`

	Private bool   `json:"-"`
	Draft   bool   `json:"-"`
	Dir     string `json:"-"`
	// Files is the deck's own directory; assets are served from it.
	Files fs.FS `json:"-"`
}

type frontmatter struct {
	Code        string         `yaml:"code"`
	Title       string         `yaml:"title"`
	Description string         `yaml:"description"`
	Theme       string         `yaml:"theme"`
	CSS         []string       `yaml:"css"`
	Private     bool           `yaml:"private"`
	Draft       bool           `yaml:"draft"`
	Reveal      map[string]any `yaml:"reveal"`
}

type Library struct {
	byCode map[string]*Deck
	byDir  map[string]*Deck
	all    []*Deck
}

// Lookup expects an already-lowercased code.
func (l *Library) Lookup(code string) (*Deck, bool) {
	d, ok := l.byCode[code]
	return d, ok
}

// All returns the decks sorted by code.
func (l *Library) All() []*Deck { return l.all }

type Options struct {
	// IncludeDrafts serves `draft: true` decks (dev mode). They are always
	// validated either way.
	IncludeDrafts bool
}

// Load reads every deck directory at the root of fsys and fails if any deck
// is invalid.
func Load(fsys fs.FS, opts Options) (*Library, error) {
	lib, errs := load(fsys, opts, nil)
	if len(errs) > 0 {
		return nil, errors.Join(errs...)
	}
	return lib, nil
}

// Reload reads fsys like Load, but a deck that fails to load keeps the
// version prev had for the same directory (if any). The returned error lists
// every problem; the library is always usable. If the directory itself is
// unreadable, prev is returned unchanged.
func Reload(prev *Library, fsys fs.FS, opts Options) (*Library, error) {
	lib, errs := load(fsys, opts, prev)
	if lib == nil {
		return prev, errors.Join(errs...)
	}
	return lib, errors.Join(errs...)
}

func load(fsys fs.FS, opts Options, prev *Library) (*Library, []error) {
	entries, err := fs.ReadDir(fsys, ".")
	if err != nil {
		return nil, []error{fmt.Errorf("reading presentations: %w", err)}
	}

	lib := &Library{byCode: map[string]*Deck{}, byDir: map[string]*Deck{}}
	owner := map[string]string{}
	var errs []error

	for _, e := range entries {
		name := e.Name()
		if !e.IsDir() || strings.HasPrefix(name, "_") || strings.HasPrefix(name, ".") {
			continue
		}
		deck, err := loadDeck(fsys, name)
		if err != nil {
			errs = append(errs, fmt.Errorf("%s: %w", name, err))
			last := prev.dir(name)
			if last == nil {
				continue
			}
			deck = last // keep serving the last good version
		}
		if other, dup := owner[deck.Code]; dup {
			errs = append(errs, fmt.Errorf("%s: code %q is already used by %s", name, deck.Code, other))
			continue
		}
		owner[deck.Code] = name
		if deck.Draft && !opts.IncludeDrafts {
			continue
		}
		lib.byCode[deck.Code] = deck
		lib.byDir[name] = deck
		lib.all = append(lib.all, deck)
	}
	sort.Slice(lib.all, func(i, j int) bool { return lib.all[i].Code < lib.all[j].Code })
	return lib, errs
}

func (l *Library) dir(name string) *Deck {
	if l == nil {
		return nil
	}
	return l.byDir[name]
}

func loadDeck(fsys fs.FS, dir string) (*Deck, error) {
	files, err := fs.Sub(fsys, dir)
	if err != nil {
		return nil, err
	}

	var (
		source string
		format Format
	)
	for _, c := range []struct {
		name   string
		format Format
	}{{"index.md", Markdown}, {"index.html", HTML}} {
		if IsPlainFile(files, c.name) {
			if source != "" {
				return nil, errors.New("has both index.md and index.html; keep one")
			}
			source, format = c.name, c.format
		}
	}
	if source == "" {
		return nil, errors.New("no index.md or index.html")
	}

	raw, err := fs.ReadFile(files, source)
	if err != nil {
		return nil, err
	}
	meta, body, err := splitFrontmatter(raw)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", source, err)
	}

	var fm frontmatter
	dec := yaml.NewDecoder(bytes.NewReader(meta))
	// A typo like `privat: true` must fail loudly, not silently publish.
	dec.KnownFields(true)
	if err := dec.Decode(&fm); err != nil {
		return nil, fmt.Errorf("%s frontmatter: %w", source, err)
	}

	d := &Deck{
		Code:        fm.Code,
		Title:       strings.TrimSpace(fm.Title),
		Description: strings.TrimSpace(fm.Description),
		Theme:       fm.Theme,
		CSS:         fm.CSS,
		Reveal:      fm.Reveal,
		Format:      format,
		Content:     body,
		Private:     fm.Private,
		Draft:       fm.Draft,
		Dir:         dir,
		Files:       files,
	}
	if d.Theme == "" {
		d.Theme = DefaultTheme
	}
	return d, d.validate()
}

func (d *Deck) validate() error {
	var errs []error
	switch {
	case d.Code == "":
		errs = append(errs, errors.New("frontmatter: code is required"))
	case !codePattern.MatchString(d.Code):
		errs = append(errs, fmt.Errorf("code %q: use 3–64 lowercase letters, digits and inner hyphens", d.Code))
	case slices.Contains(reservedCodes, d.Code):
		errs = append(errs, fmt.Errorf("code %q is reserved", d.Code))
	case d.Private && len(d.Code) < PrivateMinLength:
		errs = append(errs, fmt.Errorf("code %q: private decks need a code of at least %d characters", d.Code, PrivateMinLength))
	}
	if d.Title == "" {
		errs = append(errs, errors.New("frontmatter: title is required"))
	}
	if !slices.Contains(Themes, d.Theme) {
		errs = append(errs, fmt.Errorf("theme %q: pick one of %s", d.Theme, strings.Join(Themes, ", ")))
	}
	for _, css := range d.CSS {
		if !IsAssetPath(css) || !IsPlainFile(d.Files, css) {
			errs = append(errs, fmt.Errorf("css %q: not a file inside the deck directory", css))
		}
	}
	if strings.TrimSpace(d.Content) == "" {
		errs = append(errs, errors.New("has no slides"))
	}
	return errors.Join(errs...)
}

// IsAssetPath reports whether name may be served from a deck directory: a
// clean relative path, no dot-files or dot-directories, and not the deck
// source itself.
func IsAssetPath(name string) bool {
	if !fs.ValidPath(name) || name == "." {
		return false
	}
	if name == "index.md" || name == "index.html" {
		return false
	}
	for seg := range strings.SplitSeq(name, "/") {
		if strings.HasPrefix(seg, ".") {
			return false
		}
	}
	return path.Clean(name) == name
}

// IsPlainFile reports whether name is a regular file reached without any
// symlink along the way.
func IsPlainFile(fsys fs.FS, name string) bool {
	if !fs.ValidPath(name) || name == "." {
		return false
	}
	prefix := ""
	for seg := range strings.SplitSeq(name, "/") {
		prefix = path.Join(prefix, seg)
		info, err := fs.Lstat(fsys, prefix)
		if err != nil || info.Mode()&fs.ModeSymlink != 0 {
			return false
		}
		if prefix == name {
			return info.Mode().IsRegular()
		}
	}
	return false
}

// splitFrontmatter separates a leading `---` YAML block from the body.
func splitFrontmatter(src []byte) (meta []byte, body string, err error) {
	s := strings.ReplaceAll(string(src), "\r\n", "\n")
	s = strings.TrimPrefix(s, "\uFEFF")
	if !strings.HasPrefix(s, "---\n") {
		return nil, "", errors.New("must start with a --- frontmatter block (code, title, …)")
	}
	rest := s[len("---\n"):]
	end := strings.Index(rest, "\n---\n")
	switch {
	case end >= 0:
		return []byte(rest[:end]), rest[end+len("\n---\n"):], nil
	case strings.HasSuffix(rest, "\n---"):
		return []byte(strings.TrimSuffix(rest, "\n---")), "", nil
	default:
		return nil, "", errors.New("frontmatter block is not closed with ---")
	}
}
