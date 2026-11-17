package netx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestBanlist(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	b := NewBanlist(3, 10*time.Minute, time.Hour)
	b.now = func() time.Time { return now }
	var bannedIP netip.Addr
	b.OnBan = func(ip netip.Addr, _ time.Time) { bannedIP = ip }
	ip := netip.MustParseAddr("203.0.113.9")

	b.Fail(ip)
	b.Fail(ip)
	if banned, _ := b.Banned(ip); banned {
		t.Fatal("banned below the threshold")
	}
	// Failures older than the window are forgotten.
	now = now.Add(11 * time.Minute)
	b.Fail(ip)
	if banned, _ := b.Banned(ip); banned {
		t.Fatal("old failures counted")
	}
	b.Fail(ip)
	b.Fail(ip)
	banned, left := b.Banned(ip)
	if !banned || left != time.Hour || bannedIP != ip {
		t.Fatalf("banned=%v left=%v cb=%v", banned, left, bannedIP)
	}
	if other, _ := b.Banned(netip.MustParseAddr("203.0.113.10")); other {
		t.Fatal("another client is banned")
	}
	now = now.Add(time.Hour + time.Second)
	if banned, _ := b.Banned(ip); banned {
		t.Fatal("ban did not expire")
	}

	// IPv6 clients are counted per /64; private and loopback addresses never.
	for _, a := range []string{"2001:db8:1:2::1", "2001:db8:1:2::2", "2001:db8:1:2:ffff::3"} {
		b.Fail(netip.MustParseAddr(a))
	}
	if banned, _ := b.Banned(netip.MustParseAddr("2001:db8:1:2::99")); !banned {
		t.Fatal("ipv6 /64 not banned together")
	}
	if banned, _ := b.Banned(netip.MustParseAddr("2001:db8:1:3::1")); banned {
		t.Fatal("neighbouring /64 banned")
	}
	for _, a := range []string{"172.18.0.1", "192.168.1.20", "127.0.0.1", "10.0.0.5"} {
		lan := netip.MustParseAddr(a)
		for range 10 {
			b.Fail(lan)
		}
		if banned, _ := b.Banned(lan); banned {
			t.Fatalf("%s banned", a)
		}
	}
}

func TestGuard(t *testing.T) {
	b := NewBanlist(2, time.Minute, time.Hour)
	status := http.StatusUnauthorized
	h := Middleware(nil)(Guard(b, func(r *http.Request) bool { return r.URL.Path == "/login" })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(status) })))
	call := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, path, nil)
		req.RemoteAddr = "198.51.100.7:4000"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}
	// Failures elsewhere do not count.
	for range 5 {
		call("/other")
	}
	if rec := call("/login"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("first: %d", rec.Code)
	}
	call("/login")
	rec := call("/login")
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("after two failures: %d %v", rec.Code, rec.Header())
	}
	// Other paths still work for a banned client.
	status = http.StatusOK
	if rec := call("/other"); rec.Code != http.StatusOK {
		t.Fatalf("unguarded path: %d", rec.Code)
	}
	// Successes never count.
	b2 := NewBanlist(2, time.Minute, time.Hour)
	h = Middleware(nil)(Guard(b2, func(*http.Request) bool { return true })(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("ok")) })))
	for range 5 {
		if rec := call("/login"); rec.Code != http.StatusOK {
			t.Fatalf("success: %d", rec.Code)
		}
	}
}
