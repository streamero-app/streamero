package netx

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func req(remote string, xff ...string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.RemoteAddr = remote
	for _, v := range xff {
		r.Header.Add("X-Forwarded-For", v)
	}
	return r
}

func TestClientIP(t *testing.T) {
	proxies, err := ParsePrefixes("172.16.0.0/12, 127.0.0.1")
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name string
		r    *http.Request
		want string
	}{
		{"direct client ignores XFF", req("203.0.113.9:1234", "1.2.3.4"), "203.0.113.9"},
		{"trusted proxy uses XFF", req("172.18.0.1:5555", "198.51.100.7"), "198.51.100.7"},
		{"spoofed left-most entry ignored", req("172.18.0.1:5555", "6.6.6.6, 198.51.100.7"), "198.51.100.7"},
		{"chained trusted proxies skipped", req("127.0.0.1:1", "198.51.100.7, 172.20.0.5"), "198.51.100.7"},
		{"multiple headers joined", req("127.0.0.1:1", "6.6.6.6", "198.51.100.8"), "198.51.100.8"},
		{"malformed chain falls back to peer", req("172.18.0.1:5555", "not-an-ip"), "172.18.0.1"},
		{"no XFF from trusted proxy", req("172.18.0.1:5555"), "172.18.0.1"},
		{"ipv4-mapped ipv6 peer", req("[::ffff:172.18.0.1]:5555", "198.51.100.9"), "198.51.100.9"},
	}
	for _, c := range cases {
		if got := ClientIP(c.r, proxies).String(); got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if _, err := ParsePrefixes("10.0.0.0/8,nope"); err == nil {
		t.Error("invalid proxy accepted")
	}
}

func TestLimiter(t *testing.T) {
	l := NewLimiter(60, 3) // 1/s, burst 3
	now := time.Unix(1000, 0)
	l.now = func() time.Time { return now }
	a, b := netip.MustParseAddr("198.51.100.1"), netip.MustParseAddr("198.51.100.2")
	for i := 0; i < 3; i++ {
		if ok, _ := l.Allow(a); !ok {
			t.Fatalf("burst request %d rejected", i)
		}
	}
	if ok, wait := l.Allow(a); ok || wait <= 0 || wait > time.Second {
		t.Fatalf("over burst: ok=%v wait=%s", ok, wait)
	}
	if ok, _ := l.Allow(b); !ok {
		t.Fatal("other client limited")
	}
	now = now.Add(time.Second)
	if ok, _ := l.Allow(a); !ok {
		t.Fatal("token not refilled after 1s")
	}
}
