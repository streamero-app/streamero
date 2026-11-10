// Package discovery answers the LAN discovery broadcast that media clients of
// the compat API send to UDP port 7359 (D-033). A probe is the text
// "Who is JellyfinServer?"; the reply is a small JSON object with the server's
// address, id and name (the public "ServerDiscoveryInfo" model). Written from
// the public documentation only (clean-room, D-031).
package discovery

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"
)

// DefaultAddr is the UDP address clients broadcast to.
const DefaultAddr = ":7359"

// probe is what clients send (compared case-insensitively, "?" optional).
const probe = "who is jellyfinserver"

// Info is the reply payload.
type Info struct {
	Address         string  `json:"Address"`
	ID              string  `json:"Id"`
	Name            string  `json:"Name"`
	EndpointAddress *string `json:"EndpointAddress"`
}

// Responder answers discovery probes from the local network.
type Responder struct {
	// Info returns the server id (as clients see it) and name.
	Info func(ctx context.Context) (id, name string)
	// URL is the announced server address; empty = http://<local IP facing the client>:<HTTPPort>.
	URL      string
	HTTPPort int
	Log      *slog.Logger
	Now      func() time.Time

	mu     sync.Mutex
	last   map[netip.Addr]time.Time
	window time.Time
	count  int
}

// Rate limits: one reply per source address per perSource, at most
// maxPerSecond replies overall. A reply is a few hundred bytes, so the
// responder cannot be used to amplify traffic even if the port is reachable
// from elsewhere (only private, loopback and link-local senders get one anyway).
const (
	perSource    = 250 * time.Millisecond
	maxPerSecond = 20
)

// ListenAndServe answers probes on addr until ctx ends.
func (r *Responder) ListenAndServe(ctx context.Context, addr string) error {
	pc, err := net.ListenPacket("udp", addr)
	if err != nil {
		return err
	}
	return r.Serve(ctx, pc)
}

// Serve answers probes arriving on pc until ctx ends; it closes pc.
func (r *Responder) Serve(ctx context.Context, pc net.PacketConn) error {
	stop := context.AfterFunc(ctx, func() { _ = pc.Close() })
	defer stop()
	defer func() { _ = pc.Close() }()
	buf := make([]byte, 512)
	for {
		n, from, err := pc.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		if reply := r.Reply(ctx, buf[:n], from); reply != nil {
			if _, err := pc.WriteTo(reply, from); err != nil && r.Log != nil {
				r.Log.Debug("discovery reply failed", "err", err)
			}
		}
	}
}

// Reply returns the answer to a datagram, or nil when it gets none (not a
// probe, sender outside the local network, or rate limited).
func (r *Responder) Reply(ctx context.Context, msg []byte, from net.Addr) []byte {
	ua, ok := from.(*net.UDPAddr)
	if !ok {
		return nil
	}
	ip := ua.AddrPort().Addr().Unmap()
	if !local(ip) || !isProbe(msg) || !r.allow(ip) {
		return nil
	}
	id, name := r.Info(ctx)
	b, _ := json.Marshal(Info{Address: r.address(ua), ID: id, Name: name})
	return b
}

func local(ip netip.Addr) bool {
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

func isProbe(msg []byte) bool {
	s := strings.TrimSpace(strings.TrimRight(string(msg), "\x00"))
	s = strings.TrimSuffix(s, "?")
	return strings.EqualFold(s, probe)
}

func (r *Responder) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Responder) allow(ip netip.Addr) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if now.Sub(r.window) >= time.Second {
		r.window, r.count = now, 0
	}
	if r.count >= maxPerSecond {
		return false
	}
	if r.last == nil || len(r.last) > 4096 {
		r.last = map[netip.Addr]time.Time{}
	}
	if t, ok := r.last[ip]; ok && now.Sub(t) < perSource {
		return false
	}
	r.last[ip] = now
	r.count++
	return true
}

// address is the configured URL, or the address of the local interface that
// routes to the client (no packet is sent to find it).
func (r *Responder) address(to *net.UDPAddr) string {
	if r.URL != "" {
		return r.URL
	}
	port := r.HTTPPort
	if port == 0 {
		port = 8787
	}
	ip := netip.MustParseAddr("127.0.0.1")
	if c, err := net.DialUDP("udp", nil, to); err == nil {
		if la, ok := c.LocalAddr().(*net.UDPAddr); ok {
			ip = la.AddrPort().Addr().Unmap()
		}
		_ = c.Close()
	}
	return "http://" + netip.AddrPortFrom(ip.WithZone(""), uint16(port)).String()
}

// PortOf returns the TCP port of a listen address such as ":8787" (0 if unknown).
func PortOf(listen string) int {
	_, p, err := net.SplitHostPort(listen)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(p)
	return n
}
