package decks

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io/fs"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// Source keeps the current Library for a mounted presentations directory and
// swaps in a new one when the files change. Readers never block: Library()
// is an atomic load.
//
// Change detection is a fingerprint of every path, size and mtime — cheap
// for a folder of slides, and it catches git-sync's worktree swaps as well as
// in-place edits. Nothing is re-parsed while nothing changed.
type Source struct {
	fsys fs.FS
	opts Options
	log  *slog.Logger

	current atomic.Pointer[Library]

	mu          sync.Mutex // serializes Refresh
	fingerprint string
	problems    error
	lastLogged  string
}

// NewSource loads fsys once. It fails only if the directory cannot be read
// at all; invalid decks are logged and left out (see Reload).
func NewSource(fsys fs.FS, opts Options, log *slog.Logger) (*Source, error) {
	if log == nil {
		log = slog.Default()
	}
	s := &Source{fsys: fsys, opts: opts, log: log}
	if _, err := fs.ReadDir(fsys, "."); err != nil {
		return nil, fmt.Errorf("reading presentations: %w", err)
	}
	s.Refresh()
	return s, nil
}

// Library is the current set of decks; never nil.
func (s *Source) Library() *Library {
	return s.current.Load()
}

// Problems describes the decks that failed to load on the last refresh
// (nil when all is well).
func (s *Source) Problems() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.problems
}

// Refresh reloads if anything under the directory changed and reports
// whether it did.
func (s *Source) Refresh() bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	fp, err := fingerprint(s.fsys)
	if err != nil {
		// Typically a sync tool swapping directories mid-walk; the next
		// tick sees a settled tree.
		s.log.Warn("presentations: scan failed, keeping current decks", "err", err)
		return false
	}
	if fp == s.fingerprint && s.current.Load() != nil {
		return false
	}

	prev := s.current.Load()
	lib, err := Reload(prev, s.fsys, s.opts)
	if lib == nil {
		lib = &Library{byCode: map[string]*Deck{}, byDir: map[string]*Deck{}}
	}
	s.current.Store(lib)
	s.fingerprint = fp
	s.problems = err

	if err != nil && err.Error() != s.lastLogged {
		s.log.Error("presentations have problems; broken decks keep their last good version", "err", err)
	}
	if err == nil {
		s.lastLogged = ""
	} else {
		s.lastLogged = err.Error()
	}
	s.log.Info("presentations loaded", "count", len(lib.All()))
	return true
}

// Watch refreshes every interval, and immediately whenever poke fires (the
// server wires SIGHUP to it), until ctx is done.
func (s *Source) Watch(ctx context.Context, interval time.Duration, poke <-chan struct{}) {
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-poke:
		}
		s.Refresh()
	}
}

func fingerprint(fsys fs.FS) (string, error) {
	h := sha256.New()
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		_, _ = fmt.Fprintf(h, "%s\x00%d\x00%d\x00%d\n", p, info.Size(), info.ModTime().UnixNano(), info.Mode())
		return nil
	})
	if err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
