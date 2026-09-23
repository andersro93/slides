package web

import (
	"sync"
	"time"
)

// missLimiter counts failed code lookups per client in a fixed window. Once a
// client has missed too often, every deck route answers 429 for it until the
// window ends — valid codes included, or the limit would be a free oracle.
type missLimiter struct {
	max        int
	window     time.Duration
	maxClients int // bound on memory; beyond it new clients go untracked
	now        func() time.Time

	mu        sync.Mutex
	clients   map[string]*missWindow
	lastPrune time.Time
}

type missWindow struct {
	count int
	reset time.Time
}

func newMissLimiter(max int, window time.Duration) *missLimiter {
	return &missLimiter{max: max, window: window, maxClients: 100_000, now: time.Now, clients: map[string]*missWindow{}}
}

// Blocked reports whether the client has used up its misses.
func (l *missLimiter) Blocked(client string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	w := l.clients[client]
	return w != nil && l.now().Before(w.reset) && w.count >= l.max
}

// Miss records one failed lookup.
func (l *missLimiter) Miss(client string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	full := len(l.clients) >= l.maxClients
	if now.Sub(l.lastPrune) > l.window || (full && now.Sub(l.lastPrune) > time.Second) {
		l.prune(now)
	}
	w := l.clients[client]
	if w == nil && len(l.clients) >= l.maxClients {
		// Holding this many distinct misbehaving clients at once means a
		// distributed attack; the codes' own entropy is the defence then,
		// not this map growing without bound.
		return
	}
	if w == nil || !now.Before(w.reset) {
		w = &missWindow{reset: now.Add(l.window)}
		l.clients[client] = w
	}
	w.count++
}

func (l *missLimiter) prune(now time.Time) {
	for k, w := range l.clients {
		if !now.Before(w.reset) {
			delete(l.clients, k)
		}
	}
	l.lastPrune = now
}
