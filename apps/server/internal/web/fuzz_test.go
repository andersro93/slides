package web

import (
	"net/netip"
	"testing"
)

// clientKey parses the client-IP header (e.g. CF-Connecting-IP), so its input
// is whatever the client or proxy sends. The key it returns decides who
// shares a rate limit.
func FuzzClientKey(f *testing.F) {
	for _, s := range []string{
		"203.0.113.7", "::ffff:203.0.113.7", "2001:db8:1:2:3:4:5:6", "2001:db8::1",
		"fe80::1%eth0", "::1", "not an ip", "", "1.2.3.4, 5.6.7.8",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		key := clientKey(raw)
		addr, err := netip.ParseAddr(raw)
		if err != nil {
			if key != raw {
				t.Fatalf("clientKey(%q) = %q: unparseable input must pass through", raw, key)
			}
			return
		}
		if clientKey(key) != key {
			t.Fatalf("clientKey(%q) = %q is not stable", raw, key)
		}
		addr = addr.Unmap()
		if addr.Is4() {
			if key != addr.String() {
				t.Fatalf("clientKey(%q) = %q, want the IPv4 address %q", raw, key, addr)
			}
			return
		}
		// Every IPv6 address in the same /64 must share one key.
		if p, err := addr.Prefix(64); err == nil {
			if key != p.String() {
				t.Fatalf("clientKey(%q) = %q, want the /64 %q", raw, key, p)
			}
		}
	})
}
