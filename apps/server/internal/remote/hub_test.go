package remote

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

type client struct {
	t *testing.T
	c *websocket.Conn
}

func newServer(t *testing.T, h *Hub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// /<code>/_ws
		code := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		h.Serve(w, r, code, "client-"+r.URL.Query().Get("client"), nil)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func dial(t *testing.T, srv *httptest.Server, code string) *client {
	t.Helper()
	return dialAs(t, srv, code, "")
}

func dialAs(t *testing.T, srv *httptest.Server, code, clientID string) *client {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/"+code+"/_ws?client="+clientID, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.CloseNow() })
	return &client{t: t, c: c}
}

func (c *client) send(v any) {
	c.t.Helper()
	data, _ := json.Marshal(v)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.c.Write(ctx, websocket.MessageText, data); err != nil {
		c.t.Fatal(err)
	}
}

func (c *client) recv() map[string]any {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, data, err := c.c.Read(ctx)
	if err != nil {
		c.t.Fatalf("recv: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(data, &m); err != nil {
		c.t.Fatal(err)
	}
	return m
}

// recvType skips messages until one of the given type arrives.
func (c *client) recvType(typ string) map[string]any {
	c.t.Helper()
	for {
		if m := c.recv(); m["type"] == typ {
			return m
		}
	}
}

func (c *client) closedWith(code websocket.StatusCode) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, _, err := c.c.Read(ctx)
		if err == nil {
			continue
		}
		if got := websocket.CloseStatus(err); got != code {
			c.t.Fatalf("close status = %v (%v), want %v", got, err, code)
		}
		return
	}
}

func host(t *testing.T, srv *httptest.Server, code, hostKey string) (*client, map[string]any) {
	t.Helper()
	h := dial(t, srv, code)
	h.send(map[string]string{"type": "host", "hostKey": hostKey})
	return h, h.recvType("hello")
}

func TestPairAndRelay(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))

	h, hi := host(t, srv, "talk", "")
	token, _ := hi["token"].(string)
	if token == "" || hi["hostKey"] == "" || hi["peer"] != false {
		t.Fatalf("host hello = %v", hi)
	}

	// The deck publishes state before any phone exists; a joining phone
	// must still get it immediately.
	h.send(map[string]any{"type": "state", "index": 3, "notes": "<p>hi</p>"})

	r := dial(t, srv, "talk")
	r.send(map[string]string{"type": "join", "token": token})
	if m := r.recvType("hello"); m["role"] != "remote" || m["peer"] != true {
		t.Fatalf("remote hello = %v", m)
	}
	if m := r.recvType("state"); m["index"] != float64(3) {
		t.Fatalf("cached state = %v", m)
	}
	if m := h.recvType("peer"); m["connected"] != true {
		t.Fatalf("host peer = %v", m)
	}

	r.send(map[string]string{"type": "cmd", "action": "next"})
	if m := h.recvType("cmd"); m["action"] != "next" {
		t.Fatalf("host got %v", m)
	}

	h.send(map[string]any{"type": "state", "index": 4})
	if m := r.recvType("state"); m["index"] != float64(4) {
		t.Fatalf("remote got %v", m)
	}
}

func TestDirectionIsEnforced(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	h, hi := host(t, srv, "talk", "")
	r := dial(t, srv, "talk")
	r.send(map[string]string{"type": "join", "token": hi["token"].(string)})
	r.recvType("hello")
	h.recvType("peer")

	// A remote cannot push state to the deck's audience-facing side, and a
	// host "cmd" goes nowhere.
	r.send(map[string]any{"type": "state", "notes": "<script>x</script>"})
	h.send(map[string]any{"type": "cmd", "action": "next"})
	r.send(map[string]string{"type": "cmd", "action": "prev"})

	if m := h.recv(); m["type"] != "cmd" || m["action"] != "prev" {
		t.Fatalf("host got %v, want only the remote's cmd", m)
	}
	h.send(map[string]any{"type": "state", "index": 9})
	if m := r.recv(); m["type"] != "state" || m["index"] != float64(9) {
		t.Fatalf("remote got %v, want only the host's state", m)
	}
}

func TestUnknownTokenAndWrongDeck(t *testing.T) {
	var misses int
	h := NewHub(Options{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		code := strings.Split(strings.TrimPrefix(r.URL.Path, "/"), "/")[0]
		h.Serve(w, r, code, "c", func() { misses++ })
	}))
	t.Cleanup(srv.Close)

	_, hi := host(t, srv, "talk", "")

	r := dial(t, srv, "talk")
	r.send(map[string]string{"type": "join", "token": "nope"})
	r.closedWith(CloseUnknownToken)

	// A valid token is scoped to the deck it was issued for.
	other := dial(t, srv, "other-deck")
	other.send(map[string]string{"type": "join", "token": hi["token"].(string)})
	other.closedWith(CloseUnknownToken)

	if misses != 2 {
		t.Fatalf("misses = %d, want 2", misses)
	}
}

func TestHostResumesWithHostKey(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	h1, hi := host(t, srv, "talk", "")
	token, key := hi["token"].(string), hi["hostKey"].(string)

	r := dial(t, srv, "talk")
	r.send(map[string]string{"type": "join", "token": token})
	r.recvType("hello")

	// The deck reloads: same host key, same session, phone stays paired.
	_ = h1.c.Close(websocket.StatusNormalClosure, "reload")
	if m := r.recvType("peer"); m["connected"] != false {
		t.Fatalf("remote peer = %v", m)
	}
	h2, hi2 := host(t, srv, "talk", key)
	if hi2["hostKey"] != key || hi2["peer"] != true {
		t.Fatalf("resumed hello = %v", hi2)
	}
	if m := r.recvType("peer"); m["connected"] != true {
		t.Fatalf("remote peer = %v", m)
	}
	r.send(map[string]string{"type": "cmd", "action": "next"})
	if m := h2.recvType("cmd"); m["action"] != "next" {
		t.Fatalf("host got %v", m)
	}

	// A host key from another deck does not resume anything.
	_, hi3 := host(t, srv, "other-deck", key)
	if hi3["hostKey"] == key {
		t.Fatal("host key crossed decks")
	}
}

func pair(t *testing.T, srv *httptest.Server, code, token string) (*client, string) {
	t.Helper()
	r := dial(t, srv, code)
	r.send(map[string]string{"type": "join", "token": token})
	m := r.recvType("hello")
	key, _ := m["remoteKey"].(string)
	if key == "" {
		t.Fatalf("remote hello without remoteKey: %v", m)
	}
	return r, key
}

// The QR is on the projector; the audience can photograph it. Using it must
// spend it.
func TestPairingTokenIsSingleUse(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	h, hi := host(t, srv, "talk", "")
	qr := hi["token"].(string)

	pair(t, srv, "talk", qr)
	if m := h.recvType("token"); m["token"] == qr || m["token"] == "" {
		t.Fatalf("token not replaced after pairing: %v", m)
	}

	thief := dial(t, srv, "talk")
	thief.send(map[string]string{"type": "join", "token": qr})
	thief.closedWith(CloseUnknownToken)
}

func TestRemoteReconnectsWithItsKey(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	h, hi := host(t, srv, "talk", "")
	r1, key := pair(t, srv, "talk", hi["token"].(string))

	// The phone woke up with a new socket while the old one is still open.
	r2 := dial(t, srv, "talk")
	r2.send(map[string]string{"type": "join", "remoteKey": key})
	if m := r2.recvType("hello"); m["remoteKey"] != key {
		t.Fatalf("reconnect hello = %v", m)
	}
	r1.closedWith(CloseReplaced)

	r2.send(map[string]string{"type": "cmd", "action": "next"})
	if m := h.recvType("cmd"); m["action"] != "next" {
		t.Fatalf("host got %v", m)
	}
}

func TestNewPairingReplacesRemote(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	h, hi := host(t, srv, "talk", "")
	old, oldKey := pair(t, srv, "talk", hi["token"].(string))
	fresh := h.recvType("token")["token"].(string)

	pair(t, srv, "talk", fresh)
	old.closedWith(CloseReplaced)

	again := dial(t, srv, "talk")
	again.send(map[string]string{"type": "join", "remoteKey": oldKey})
	again.closedWith(CloseUnknownToken)
}

func TestRotateReplacesToken(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	h, hi := host(t, srv, "talk", "")
	old := hi["token"].(string)

	h.send(map[string]string{"type": "rotate"})
	if m := h.recvType("token"); m["token"] == old || m["token"] == "" {
		t.Fatalf("rotated token = %v", m)
	}
	r := dial(t, srv, "talk")
	r.send(map[string]string{"type": "join", "token": old})
	r.closedWith(CloseUnknownToken)
}

func TestSweepAndLimits(t *testing.T) {
	now := time.Now()
	hub := NewHub(Options{MaxSessions: 1, OrphanTTL: time.Minute, now: func() time.Time { return now }})
	srv := newServer(t, hub)

	h, hi := host(t, srv, "talk", "")
	r := dial(t, srv, "talk")
	r.send(map[string]string{"type": "join", "token": hi["token"].(string)})
	r.recvType("hello")

	full := dial(t, srv, "talk")
	full.send(map[string]string{"type": "host"})
	full.closedWith(CloseBusy)

	_ = h.c.Close(websocket.StatusNormalClosure, "bye")
	r.recvType("peer")

	hub.Sweep() // within TTL: kept
	if hub.Sessions() != 1 {
		t.Fatalf("sessions = %d, want 1", hub.Sessions())
	}
	now = now.Add(2 * time.Minute)
	hub.Sweep()
	if hub.Sessions() != 0 {
		t.Fatalf("sessions = %d, want 0", hub.Sessions())
	}
	r.closedWith(CloseUnknownToken)
}

func TestBadHello(t *testing.T) {
	srv := newServer(t, NewHub(Options{}))
	c := dial(t, srv, "talk")
	c.send(map[string]string{"type": "cmd"})
	c.closedWith(CloseBadHello)
}

func TestCloseSaysGoingAway(t *testing.T) {
	hub := NewHub(Options{})
	srv := newServer(t, hub)
	h, _ := host(t, srv, "talk", "")
	hub.Close()
	h.closedWith(websocket.StatusGoingAway)

	// After Close, new sockets are turned away the same way.
	c := dial(t, srv, "talk")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, _, err := c.c.Read(ctx)
	if websocket.CloseStatus(err) != websocket.StatusGoingAway {
		t.Fatalf("err = %v", err)
	}
}

func TestPerClientSessionCap(t *testing.T) {
	hub := NewHub(Options{MaxSessionsPerClient: 2})
	srv := newServer(t, hub)

	hostAs := func(id string) *client {
		c := dialAs(t, srv, "talk", id)
		c.send(map[string]string{"type": "host"})
		return c
	}
	a1, a2 := hostAs("a"), hostAs("a")
	a1.recvType("hello")
	a2.recvType("hello")

	// A third live session for the same client is refused…
	hostAs("a").closedWith(CloseBusy)
	// …while another client is unaffected.
	hostAs("b").recvType("hello")

	// Abandoned sessions of that client make room again.
	_ = a1.c.Close(websocket.StatusNormalClosure, "bye")
	deadline := time.Now().Add(5 * time.Second)
	for {
		c := hostAs("a")
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		_, data, err := c.c.Read(ctx)
		cancel()
		if err == nil && strings.Contains(string(data), `"hello"`) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("orphaned session was not evicted: %v", err)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestUnpairedSessionsExpireFast(t *testing.T) {
	now := time.Now()
	hub := NewHub(Options{UnpairedTTL: time.Minute, OrphanTTL: time.Hour, now: func() time.Time { return now }})
	srv := newServer(t, hub)

	unpaired, _ := host(t, srv, "talk", "")
	paired, hi := host(t, srv, "talk", "")
	r, _ := pair(t, srv, "talk", hi["token"].(string))
	paired.recvType("peer")

	_ = unpaired.c.Close(websocket.StatusNormalClosure, "bye")
	_ = paired.c.Close(websocket.StatusNormalClosure, "bye")
	r.recvType("peer")
	waitFor(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		for _, s := range hub.byHostKey {
			if s.host != nil {
				return false
			}
		}
		return true
	})

	now = now.Add(2 * time.Minute)
	hub.Sweep()
	if hub.Sessions() != 1 {
		t.Fatalf("sessions = %d, want only the paired one left", hub.Sessions())
	}
}

func TestFullTableEvictsOrphans(t *testing.T) {
	hub := NewHub(Options{MaxSessions: 1})
	srv := newServer(t, hub)
	h, _ := host(t, srv, "talk", "")
	_ = h.c.Close(websocket.StatusNormalClosure, "bye")
	waitFor(t, func() bool {
		hub.mu.Lock()
		defer hub.mu.Unlock()
		for _, s := range hub.byHostKey {
			return s.host == nil
		}
		return false
	})
	// The table is full, but only of an orphan: a new deck gets in.
	host(t, srv, "talk", "")
	if hub.Sessions() != 1 {
		t.Fatalf("sessions = %d", hub.Sessions())
	}
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not met in time")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
