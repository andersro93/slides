// Package remote pairs a presenting browser (the "host": the deck on the
// meeting-room computer) with a phone (the "remote") over WebSockets, and
// relays between them: navigation commands phone → deck, slide state and
// speaker notes deck → phone.
//
// Pairing is a capability, not a login, and it is designed around the QR
// code being on the projector where the audience can photograph it:
//
//   - The host opens a socket and gets a fresh session with two secrets: a
//     pairing token, which the deck shows as a QR code when the presenter
//     presses R, and a host key, which never leaves the host's
//     sessionStorage and lets a reloaded deck resume the same session.
//   - The pairing token is single-use. The phone that presents it becomes
//     THE remote and receives its own remote key (kept in its localStorage)
//     for every later reconnect; the token is replaced at once, so a photo of
//     the QR is worthless. Pairing again (a fresh QR) replaces the remote.
//   - A phone reconnecting with its remote key replaces its own stale socket,
//     so a phone that slept is never locked out by itself.
//
// Direction is enforced here, not trusted to clients: only the host may send
// "state", only the remote may send "cmd". Sessions are scoped to the deck
// code they were created under. Anyone who knows a code can create a host
// session, so what a host sends is untrusted on the phone (the remote page
// sanitizes speaker notes), and sessions are capped per client.
//
// Every message to a socket goes through that socket's own queue, enqueued
// while the hub lock is held, so messages about one session arrive in the
// order the hub decided them (a "peer: false" can never overtake a later
// "peer: true").
//
// Sessions live in memory. A server restart drops them; the deck silently
// starts a new one and the phone is asked to scan again.
package remote

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"log/slog"
	"net/http"
	"sort"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// Close codes the clients act on (4000–4999 is the application range).
const (
	CloseBadHello      websocket.StatusCode = 4000
	CloseUnknownToken  websocket.StatusCode = 4001 // remote: pairing gone, scan again
	CloseReplaced      websocket.StatusCode = 4002 // another socket took this role; do not reconnect
	CloseBusy          websocket.StatusCode = 4003 // session limits reached; retry later
	closeHelloTimedOut websocket.StatusCode = 4004
	closeSlow          websocket.StatusCode = 4005 // could not keep up with its queue
)

const (
	readLimit    = 256 << 10 // speaker notes can be long; keep it bounded
	helloTimeout = 10 * time.Second
	writeTimeout = 5 * time.Second
	queueSize    = 64
	// Cloudflare drops WebSockets idle for 100 s; ping well inside that.
	pingInterval = 25 * time.Second
)

type Options struct {
	Logger *slog.Logger
	// MaxSessions caps concurrent sessions (default 256).
	MaxSessions int
	// MaxSessionsPerClient caps sessions one client address may hold
	// (default 8): a reloading deck reuses its session, so real use needs
	// one or two.
	MaxSessionsPerClient int
	// OrphanTTL is how long a paired session survives without its host —
	// long enough for a laptop to reboot (default 1 h).
	OrphanTTL time.Duration
	// UnpairedTTL is the same for a session no phone ever joined
	// (default 2 min): it holds nothing worth keeping.
	UnpairedTTL time.Duration
	// OriginPatterns are extra allowed Origin hosts (default: same host only).
	OriginPatterns []string
	now            func() time.Time
}

type Hub struct {
	log            *slog.Logger
	maxSessions    int
	maxPerClient   int
	orphanTTL      time.Duration
	unpairedTTL    time.Duration
	originPatterns []string
	now            func() time.Time

	mu          sync.Mutex
	byToken     map[string]*session
	byHostKey   map[string]*session
	byRemoteKey map[string]*session
	perClient   map[string]int
	peers       map[*peer]struct{}
	closed      bool
}

type session struct {
	code      string
	owner     string // client address that created it
	token     string // pairing token (QR), single-use
	hostKey   string
	remoteKey string // the paired phone's reconnect key
	paired    bool   // a phone has joined at least once
	host      *peer
	remote    *peer
	lastState []byte
	// When the host left; zero while it is connected.
	orphanedAt time.Time
}

// peer is one socket and its outgoing queue, drained by its own writer.
type peer struct {
	conn *websocket.Conn
	out  chan []byte
}

func NewHub(o Options) *Hub {
	h := &Hub{
		log:            o.Logger,
		maxSessions:    o.MaxSessions,
		maxPerClient:   o.MaxSessionsPerClient,
		orphanTTL:      o.OrphanTTL,
		unpairedTTL:    o.UnpairedTTL,
		originPatterns: o.OriginPatterns,
		now:            o.now,
		byToken:        map[string]*session{},
		byHostKey:      map[string]*session{},
		byRemoteKey:    map[string]*session{},
		perClient:      map[string]int{},
		peers:          map[*peer]struct{}{},
	}
	if h.log == nil {
		h.log = slog.Default()
	}
	if h.maxSessions == 0 {
		h.maxSessions = 256
	}
	if h.maxPerClient == 0 {
		h.maxPerClient = 8
	}
	if h.orphanTTL == 0 {
		h.orphanTTL = time.Hour
	}
	if h.unpairedTTL == 0 {
		h.unpairedTTL = 2 * time.Minute
	}
	if h.now == nil {
		h.now = time.Now
	}
	return h
}

// envelope is the part of every message the hub reads; everything else is
// passed through untouched.
type envelope struct {
	Type      string `json:"type"`
	Token     string `json:"token,omitempty"`
	HostKey   string `json:"hostKey,omitempty"`
	RemoteKey string `json:"remoteKey,omitempty"`
}

type hello struct {
	Type      string `json:"type"` // "hello"
	Role      string `json:"role"` // "host" | "remote"
	Token     string `json:"token,omitempty"`
	HostKey   string `json:"hostKey,omitempty"`
	RemoteKey string `json:"remoteKey,omitempty"`
	Peer      bool   `json:"peer"`
}

type peerMsg struct {
	Type      string `json:"type"` // "peer"
	Connected bool   `json:"connected"`
}

type tokenMsg struct {
	Type  string `json:"type"` // "token"
	Token string `json:"token"`
}

// Serve upgrades the request and runs the socket until it closes. client is
// the caller's address (for per-client limits); onBadToken is called when a
// remote presents a token or key that does not exist, so the caller can
// count it against the client's rate limit.
func (h *Hub) Serve(w http.ResponseWriter, r *http.Request, code, client string, onBadToken func()) {
	c, err := websocket.Accept(w, r, &websocket.AcceptOptions{OriginPatterns: h.originPatterns})
	if err != nil {
		return // Accept has already written the HTTP error.
	}
	c.SetReadLimit(readLimit)

	defer func() { _ = c.CloseNow() }()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	p := &peer{conn: c, out: make(chan []byte, queueSize)}
	if !h.track(p) {
		_ = c.Close(websocket.StatusGoingAway, "shutting down")
		return
	}
	defer h.untrack(p)
	go p.writeLoop(ctx)

	helloCtx, helloCancel := context.WithTimeout(ctx, helloTimeout)
	_, data, err := c.Read(helloCtx)
	helloCancel()
	if err != nil {
		_ = c.Close(closeHelloTimedOut, "no hello")
		return
	}
	var env envelope
	if json.Unmarshal(data, &env) != nil {
		_ = c.Close(CloseBadHello, "bad hello")
		return
	}

	switch env.Type {
	case "host":
		s := h.attachHost(code, client, env.HostKey, p)
		if s == nil {
			_ = c.Close(CloseBusy, "busy")
			return
		}
		go keepalive(ctx, c)
		h.hostLoop(ctx, s, p)
		h.detachHost(s, p)
	case "join":
		s := h.attachRemote(code, env, p)
		if s == nil {
			if onBadToken != nil {
				onBadToken()
			}
			_ = c.Close(CloseUnknownToken, "unknown session")
			return
		}
		go keepalive(ctx, c)
		h.remoteLoop(ctx, s, p)
		h.detachRemote(s, p)
	default:
		_ = c.Close(CloseBadHello, "bad hello")
	}
}

func (h *Hub) attachHost(code, client, hostKey string, p *peer) *session {
	h.mu.Lock()
	defer h.mu.Unlock()

	s := h.byHostKey[hostKey]
	if s == nil || s.code != code {
		if h.perClient[client] >= h.maxPerClient {
			h.evictOrphansLocked(client)
		}
		if len(h.byHostKey) >= h.maxSessions {
			h.evictOrphansLocked("")
		}
		if h.perClient[client] >= h.maxPerClient || len(h.byHostKey) >= h.maxSessions {
			return nil
		}
		s = &session{code: code, owner: client, token: rand.Text(), hostKey: rand.Text()}
		h.byToken[s.token] = s
		h.byHostKey[s.hostKey] = s
		h.perClient[client]++
	}
	if old := s.host; old != nil {
		go closeConn(old.conn, CloseReplaced, "replaced by a newer host")
	}
	s.host = p
	s.orphanedAt = time.Time{}
	h.sendLocked(p, hello{Type: "hello", Role: "host", Token: s.token, HostKey: s.hostKey, Peer: s.remote != nil})
	h.sendLocked(s.remote, peerMsg{Type: "peer", Connected: true})
	return s
}

// attachRemote joins with either a pairing token (first contact, from the
// QR) or the remote key a previous pairing handed out (reconnect).
func (h *Hub) attachRemote(code string, env envelope, p *peer) *session {
	h.mu.Lock()
	defer h.mu.Unlock()

	var s *session
	pairing := false
	switch {
	case env.RemoteKey != "":
		s = h.byRemoteKey[env.RemoteKey]
	case env.Token != "":
		s = h.byToken[env.Token]
		pairing = true
	}
	if s == nil || s.code != code {
		return nil
	}
	if pairing {
		// Spend the token and make this phone the remote.
		delete(h.byToken, s.token)
		s.token = rand.Text()
		h.byToken[s.token] = s
		delete(h.byRemoteKey, s.remoteKey)
		s.remoteKey = rand.Text()
		h.byRemoteKey[s.remoteKey] = s
		s.paired = true
	}
	if old := s.remote; old != nil {
		go closeConn(old.conn, CloseReplaced, "another remote took over")
	}
	s.remote = p
	h.sendLocked(p, hello{Type: "hello", Role: "remote", RemoteKey: s.remoteKey, Peer: s.host != nil})
	if s.lastState != nil {
		h.enqueueLocked(p, s.lastState)
	}
	if pairing {
		h.sendLocked(s.host, tokenMsg{Type: "token", Token: s.token})
	}
	h.sendLocked(s.host, peerMsg{Type: "peer", Connected: true})
	return s
}

func (h *Hub) hostLoop(ctx context.Context, s *session, p *peer) {
	for {
		_, data, err := p.conn.Read(ctx)
		if err != nil {
			return
		}
		var env envelope
		if json.Unmarshal(data, &env) != nil {
			continue
		}
		h.mu.Lock()
		if s.host != p {
			h.mu.Unlock()
			return
		}
		switch env.Type {
		case "state":
			s.lastState = data
			h.enqueueLocked(s.remote, data)
		case "rotate":
			// A fresh QR; the one on screen (or in someone's camera roll)
			// stops working. The paired phone is unaffected.
			delete(h.byToken, s.token)
			s.token = rand.Text()
			h.byToken[s.token] = s
			h.sendLocked(p, tokenMsg{Type: "token", Token: s.token})
		}
		h.mu.Unlock()
	}
}

func (h *Hub) remoteLoop(ctx context.Context, s *session, p *peer) {
	for {
		_, data, err := p.conn.Read(ctx)
		if err != nil {
			return
		}
		var env envelope
		if json.Unmarshal(data, &env) != nil || env.Type != "cmd" {
			continue
		}
		h.mu.Lock()
		if s.remote != p {
			h.mu.Unlock()
			return
		}
		h.enqueueLocked(s.host, data)
		h.mu.Unlock()
	}
}

func (h *Hub) detachHost(s *session, p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.host != p {
		return
	}
	s.host = nil
	s.orphanedAt = h.now()
	h.sendLocked(s.remote, peerMsg{Type: "peer", Connected: false})
}

func (h *Hub) detachRemote(s *session, p *peer) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s.remote != p {
		return
	}
	s.remote = nil
	h.sendLocked(s.host, peerMsg{Type: "peer", Connected: false})
}

func (h *Hub) sendLocked(p *peer, v any) {
	if p == nil {
		return
	}
	data, err := json.Marshal(v)
	if err != nil {
		h.log.Error("remote: marshal", "err", err)
		return
	}
	h.enqueueLocked(p, data)
}

// enqueueLocked queues data for p without blocking. A peer whose queue is
// full is not keeping up (a dead connection the pings have not caught yet);
// it is disconnected rather than allowed to hold anything up.
func (h *Hub) enqueueLocked(p *peer, data []byte) {
	if p == nil {
		return
	}
	select {
	case p.out <- data:
	default:
		go closeConn(p.conn, closeSlow, "too slow")
	}
}

func (p *peer) writeLoop(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case data := <-p.out:
			wctx, cancel := context.WithTimeout(ctx, writeTimeout)
			err := p.conn.Write(wctx, websocket.MessageText, data)
			cancel()
			if err != nil {
				// Surfaces as a read error in the socket's own loop, which
				// detaches it.
				_ = p.conn.CloseNow()
				return
			}
		}
	}
}

func keepalive(ctx context.Context, c *websocket.Conn) {
	t := time.NewTicker(pingInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			pctx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := c.Ping(pctx)
			cancel()
			if err != nil {
				_ = c.CloseNow()
				return
			}
		}
	}
}

// Sweep drops sessions whose host has been gone too long (OrphanTTL once a
// phone has paired, UnpairedTTL otherwise), disconnecting any remote still
// attached.
func (h *Hub) Sweep() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for _, s := range h.byHostKey {
		if s.host != nil {
			continue
		}
		ttl := h.unpairedTTL
		if s.paired {
			ttl = h.orphanTTL
		}
		if now.Sub(s.orphanedAt) > ttl {
			h.dropLocked(s, "session expired")
		}
	}
}

// evictOrphansLocked makes room by dropping host-less sessions, oldest
// first — all of them for owner, or all of them anywhere when owner is "".
// Sessions with a live host are never evicted.
func (h *Hub) evictOrphansLocked(owner string) {
	var orphans []*session
	for _, s := range h.byHostKey {
		if s.host == nil && (owner == "" || s.owner == owner) {
			orphans = append(orphans, s)
		}
	}
	sort.Slice(orphans, func(i, j int) bool { return orphans[i].orphanedAt.Before(orphans[j].orphanedAt) })
	for _, s := range orphans {
		h.dropLocked(s, "session expired")
	}
}

func (h *Hub) dropLocked(s *session, reason string) {
	delete(h.byHostKey, s.hostKey)
	delete(h.byToken, s.token)
	delete(h.byRemoteKey, s.remoteKey)
	if h.perClient[s.owner]--; h.perClient[s.owner] <= 0 {
		delete(h.perClient, s.owner)
	}
	if s.remote != nil {
		go closeConn(s.remote.conn, CloseUnknownToken, reason)
		s.remote = nil
	}
}

// Run sweeps periodically until ctx is done.
func (h *Hub) Run(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			h.Sweep()
		}
	}
}

// Close disconnects every socket with "going away", which the clients treat
// as "reconnect shortly" — the next instance is presumably coming up.
func (h *Hub) Close() {
	h.mu.Lock()
	h.closed = true
	peers := make([]*peer, 0, len(h.peers))
	for p := range h.peers {
		peers = append(peers, p)
	}
	h.mu.Unlock()
	var wg sync.WaitGroup
	for _, p := range peers {
		wg.Go(func() { closeConn(p.conn, websocket.StatusGoingAway, "server restarting") })
	}
	wg.Wait()
}

// closeConn sends a close frame and waits (bounded, inside the library) for
// the handshake. Callers that must not block on a peer that may be asleep —
// a phone with its screen off — run it in a goroutine.
func closeConn(c *websocket.Conn, code websocket.StatusCode, reason string) {
	_ = c.Close(code, reason)
}

// Sessions reports the number of live sessions (for tests and logs).
func (h *Hub) Sessions() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.byHostKey)
}

func (h *Hub) track(p *peer) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return false
	}
	h.peers[p] = struct{}{}
	return true
}

func (h *Hub) untrack(p *peer) {
	h.mu.Lock()
	delete(h.peers, p)
	h.mu.Unlock()
}
