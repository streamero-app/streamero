// Package netx resolves the real client IP behind trusted reverse proxies and
// rate-limits sensitive endpoints per client.
package netx

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"sync/atomic"
)

type ctxKey struct{}

// ParsePrefixes parses a comma-separated list of CIDRs or single IPs.
func ParsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if !strings.Contains(f, "/") {
			a, err := netip.ParseAddr(f)
			if err != nil {
				return nil, fmt.Errorf("trusted proxy %q: %w", f, err)
			}
			out = append(out, netip.PrefixFrom(a, a.BitLen()))
			continue
		}
		p, err := netip.ParsePrefix(f)
		if err != nil {
			return nil, fmt.Errorf("trusted proxy %q: %w", f, err)
		}
		out = append(out, p.Masked())
	}
	return out, nil
}

func trusted(a netip.Addr, proxies []netip.Prefix) bool {
	a = a.Unmap()
	for _, p := range proxies {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// ClientIP returns the client address. X-Forwarded-For is honoured only when
// the direct peer is a trusted proxy; then the right-most address that is not
// itself a trusted proxy wins (left-most entries are client-controlled).
func ClientIP(r *http.Request, proxies []netip.Prefix) netip.Addr {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return netip.Addr{}
	}
	peer = peer.Unmap()
	if !trusted(peer, proxies) {
		return peer
	}
	hops := strings.Split(strings.Join(r.Header.Values("X-Forwarded-For"), ","), ",")
	for i := len(hops) - 1; i >= 0; i-- {
		a, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			return peer // malformed chain: do not guess
		}
		a = a.Unmap()
		if !trusted(a, proxies) {
			return a
		}
	}
	return peer
}

// untrustedForward records that a request came with X-Forwarded-For while no
// trusted proxy is configured: the server then sees every client as the proxy,
// so rate limits, sign-in bans and lockouts apply to all clients at once.
var untrustedForward atomic.Bool

// UntrustedForwarding reports whether such a request was seen since start-up
// (shown on the admin dashboard).
func UntrustedForwarding() bool { return untrustedForward.Load() }

// Middleware stores the resolved client IP in the request context.
func Middleware(proxies []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(proxies) == 0 && r.Header.Get("X-Forwarded-For") != "" && untrustedForward.CompareAndSwap(false, true) {
				slog.Warn("requests arrive through a reverse proxy, but STREAMERO_TRUSTED_PROXIES is empty: " +
					"every client looks like the proxy, so rate limits and sign-in bans hit all clients at once. " +
					"Set STREAMERO_TRUSTED_PROXIES to the proxy's address.")
			}
			next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, ClientIP(r, proxies))))
		})
	}
}

// WithClientIP stores a client IP as Middleware does (for callers outside HTTP).
func WithClientIP(ctx context.Context, a netip.Addr) context.Context {
	return context.WithValue(ctx, ctxKey{}, a)
}

// FromContext returns the client IP stored by Middleware.
func FromContext(ctx context.Context) netip.Addr {
	a, _ := ctx.Value(ctxKey{}).(netip.Addr)
	return a
}
