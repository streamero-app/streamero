package netx

import (
	"bufio"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Banlist blocks a client from the sign-in style endpoints for a while after it
// keeps failing them: guessing passwords, setup codes, PINs or invite tokens. It
// complements the rate limiter (which only slows a client down) and the
// per-account lockout (which a guesser avoids by rotating user names).
type Banlist struct {
	mu       sync.Mutex
	failures int           // failed attempts within window that trigger a ban
	window   time.Duration // how long failures are remembered
	ban      time.Duration // how long a ban lasts
	clients  map[netip.Addr]*offender
	now      func() time.Time
	// OnBan is called (outside the lock) each time a client is banned.
	OnBan func(ip netip.Addr, until time.Time)
}

type offender struct {
	count int
	first time.Time
	until time.Time
}

// NewBanlist bans a client for ban after failures failed attempts within window.
func NewBanlist(failures int, window, ban time.Duration) *Banlist {
	return &Banlist{failures: failures, window: window, ban: ban, clients: map[netip.Addr]*offender{}, now: time.Now}
}

// Key is the address a client is counted under: IPv6 clients by their /64 (one
// subscriber owns the whole prefix, so rotating addresses inside it buys nothing).
func Key(ip netip.Addr) netip.Addr {
	ip = ip.Unmap()
	if ip.Is6() {
		if p, err := ip.Prefix(64); err == nil {
			return p.Addr()
		}
	}
	return ip
}

// exempt reports addresses that are never banned: the local network and the
// machine itself. Behind a reverse proxy without STREAMERO_TRUSTED_PROXIES every
// visitor appears as the proxy's private address, and banning it would lock
// everyone out; people on the LAN are not the threat this guards against.
func exempt(ip netip.Addr) bool {
	return !ip.IsValid() || ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast()
}

// Banned reports whether ip is banned and for how much longer.
func (b *Banlist) Banned(ip netip.Addr) (bool, time.Duration) {
	if exempt(ip) {
		return false, 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	o, ok := b.clients[Key(ip)]
	if !ok {
		return false, 0
	}
	if left := o.until.Sub(b.now()); left > 0 {
		return true, left
	}
	return false, 0
}

// Fail records a failed attempt by ip and bans it once it has too many.
func (b *Banlist) Fail(ip netip.Addr) {
	if exempt(ip) {
		return
	}
	k := Key(ip)
	b.mu.Lock()
	now := b.now()
	o, ok := b.clients[k]
	if !ok || now.Sub(o.first) > b.window {
		if len(b.clients) > 100_000 { // bound memory under address spraying
			for a, x := range b.clients {
				if now.Sub(x.first) > b.window && now.After(x.until) {
					delete(b.clients, a)
				}
			}
		}
		o = &offender{first: now}
		b.clients[k] = o
	}
	o.count++
	var banned time.Time
	if o.count >= b.failures {
		o.until, o.count, o.first = now.Add(b.ban), 0, now
		banned = o.until
	}
	cb := b.OnBan
	b.mu.Unlock()
	if !banned.IsZero() && cb != nil {
		cb(k, banned)
	}
}

// statusWriter remembers the response status and keeps streaming and upgrades working.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Write(p []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	return w.ResponseWriter.Write(p)
}

func (w *statusWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (w *statusWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if h, ok := w.ResponseWriter.(http.Hijacker); ok {
		return h.Hijack()
	}
	return nil, nil, errors.New("netx: connection cannot be hijacked")
}

func (w *statusWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

// Guard refuses the requests match selects while their client is banned (429 with
// Retry-After) and counts each one answered 401 or 403 as a failed attempt.
func Guard(b *Banlist, match func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !match(r) {
				next.ServeHTTP(w, r)
				return
			}
			ip := FromContext(r.Context())
			if banned, left := b.Banned(ip); banned {
				w.Header().Set("Retry-After", strconv.Itoa(int(left.Seconds())+1))
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{"code": "rate_limited", // the code every client already explains
					"message": "too many failed attempts from this address; try again later"})
				return
			}
			sw := &statusWriter{ResponseWriter: w}
			next.ServeHTTP(sw, r)
			if sw.status == http.StatusUnauthorized || sw.status == http.StatusForbidden {
				b.Fail(ip)
			}
		})
	}
}
