package dlna

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"net"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/ipv4"
)

// SSDP constants (UPnP Device Architecture 1.0, section 1).
const (
	ssdpGroup     = "239.255.255.250"
	ssdpPort      = 1900
	maxAge        = 1800
	announceEvery = 5 * time.Minute // well inside max-age/2
	stateCheck    = 5 * time.Second
	maxMX         = 3 // seconds; replies are spread over at most this
)

var ssdpAddr = &net.UDPAddr{IP: net.ParseIP(ssdpGroup), Port: ssdpPort}

// SSDP announces the media server and answers M-SEARCH requests. Only
// senders on the local network get answers, and replies are rate limited
// per sender and overall so the port cannot be used to amplify traffic.
type SSDP struct {
	// UDN is "uuid:…".
	UDN string
	// Location returns the description URL to announce to a client reached
	// through local address ip.
	Location func(local netip.Addr) string
	// Active reports whether to announce and answer (admin setting).
	Active func() bool
	Server string // SERVER header value
	Log    *slog.Logger

	limiter limiter
	mu      sync.Mutex
	ifaces  []string
}

// ssdpTargets are the notification types this device answers for.
func (s *SSDP) targets() []string {
	return []string{"upnp:rootdevice", s.UDN, typeMediaServer, typeCDS, typeCMS}
}

func (s *SSDP) usn(nt string) string {
	if nt == s.UDN {
		return s.UDN
	}
	return s.UDN + "::" + nt
}

// Search is a parsed M-SEARCH request.
type Search struct {
	ST string
	MX int
}

// ParseSearch parses an M-SEARCH datagram; ok is false for anything else
// (NOTIFY from other devices, garbage, a missing ssdp:discover MAN header).
func ParseSearch(msg []byte) (Search, bool) {
	req, err := http.ReadRequest(bufio.NewReader(bytes.NewReader(msg)))
	if err != nil || req.Method != "M-SEARCH" {
		return Search{}, false
	}
	if strings.Trim(strings.TrimSpace(req.Header.Get("MAN")), `"`) != "ssdp:discover" {
		return Search{}, false
	}
	st := strings.TrimSpace(req.Header.Get("ST"))
	if st == "" {
		return Search{}, false
	}
	mx, _ := strconv.Atoi(strings.TrimSpace(req.Header.Get("MX")))
	return Search{ST: st, MX: min(max(mx, 0), maxMX)}, true
}

// matches returns the targets a search asks for.
func (s *SSDP) matches(st string) []string {
	if st == "ssdp:all" {
		return s.targets()
	}
	for _, t := range s.targets() {
		if strings.EqualFold(st, t) {
			return []string{t}
		}
		// A search for an older version of a type is answered with that version.
		if base, ver, ok := cutVersion(t); ok {
			if b2, v2, ok2 := cutVersion(st); ok2 && strings.EqualFold(base, b2) && v2 >= 1 && v2 <= ver {
				return []string{st}
			}
		}
	}
	return nil
}

func cutVersion(t string) (string, int, bool) {
	i := strings.LastIndex(t, ":")
	if i < 0 || !strings.HasPrefix(t, "urn:") {
		return "", 0, false
	}
	v, err := strconv.Atoi(t[i+1:])
	return t[:i], v, err == nil
}

// Responses returns the replies to one M-SEARCH (nil when none is due).
func (s *SSDP) Responses(search Search, location string, now time.Time) [][]byte {
	var out [][]byte
	for _, st := range s.matches(search.ST) {
		nt := st
		out = append(out, []byte("HTTP/1.1 200 OK\r\n"+
			"CACHE-CONTROL: max-age="+strconv.Itoa(maxAge)+"\r\n"+
			"DATE: "+now.UTC().Format(http.TimeFormat)+"\r\n"+
			"EXT:\r\n"+
			"LOCATION: "+location+"\r\n"+
			"SERVER: "+s.Server+"\r\n"+
			"ST: "+st+"\r\n"+
			"USN: "+s.usn(nt)+"\r\n"+
			"Content-Length: 0\r\n\r\n"))
	}
	return out
}

func (s *SSDP) notify(nt, nts, location string) []byte {
	msg := "NOTIFY * HTTP/1.1\r\nHOST: " + ssdpGroup + ":" + strconv.Itoa(ssdpPort) + "\r\n"
	if nts == "ssdp:alive" {
		msg += "CACHE-CONTROL: max-age=" + strconv.Itoa(maxAge) + "\r\nLOCATION: " + location + "\r\nSERVER: " + s.Server + "\r\n"
	}
	return []byte(msg + "NT: " + nt + "\r\nNTS: " + nts + "\r\nUSN: " + s.usn(nt) + "\r\nContent-Length: 0\r\n\r\n")
}

// Interfaces returns the network interfaces joined to the SSDP group.
func (s *SSDP) Interfaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.ifaces...)
}

// ListenAndServe joins the SSDP group on every multicast interface and serves until ctx ends.
func (s *SSDP) ListenAndServe(ctx context.Context, addr string) error {
	lc := net.ListenConfig{Control: reuseAddr}
	c, err := lc.ListenPacket(ctx, "udp4", addr)
	if err != nil {
		return err
	}
	return s.Serve(ctx, c, true)
}

// Serve answers on c until ctx ends; with multicast it also joins the group
// and announces itself. It closes c.
func (s *SSDP) Serve(ctx context.Context, c net.PacketConn, multicast bool) error {
	p := ipv4.NewPacketConn(c)
	defer func() { _ = p.Close() }()
	_ = p.SetControlMessage(ipv4.FlagInterface|ipv4.FlagDst, true)
	if multicast {
		_ = p.SetMulticastTTL(2)
		_ = p.SetMulticastLoopback(true)
		s.join(p)
		done := make(chan struct{})
		defer func() { <-done }()
		// The announcer closes the socket itself, after its ssdp:byebye is out.
		go func() { defer close(done); s.announce(ctx, p); _ = p.Close() }()
	} else {
		stop := context.AfterFunc(ctx, func() { _ = p.Close() })
		defer stop()
	}
	buf := make([]byte, 2048)
	for {
		n, cm, from, err := p.ReadFrom(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
		ua, ok := from.(*net.UDPAddr)
		if !ok || !s.Active() {
			continue
		}
		src := ua.AddrPort().Addr().Unmap()
		search, ok := ParseSearch(buf[:n])
		if !ok || !isLocal(src) || !s.limiter.allow(src, time.Now()) {
			continue
		}
		ifIndex := 0
		if cm != nil {
			ifIndex = cm.IfIndex
		}
		replies := s.Responses(search, s.Location(localAddrFor(src, ifIndex)), time.Now())
		if len(replies) == 0 {
			continue
		}
		send := func() {
			for _, r := range replies {
				if _, err := p.WriteTo(r, nil, ua); err != nil && s.Log != nil {
					s.Log.Debug("ssdp reply failed", "to", ua.String(), "err", err)
				}
			}
		}
		if search.MX > 0 {
			// #nosec G404 -- jitter only
			time.AfterFunc(time.Duration(rand.Int64N(int64(search.MX)*int64(time.Second))), send)
		} else {
			send()
		}
	}
}

// join adds every up, multicast-capable IPv4 interface to the group.
func (s *SSDP) join(p *ipv4.PacketConn) {
	ifs, _ := net.Interfaces()
	var joined []string
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 || ipv4Of(&ifi) == nil {
			continue
		}
		if err := p.JoinGroup(&ifi, &net.UDPAddr{IP: ssdpAddr.IP}); err == nil {
			joined = append(joined, ifi.Name)
		} else if s.Log != nil && !strings.Contains(err.Error(), "address already in use") {
			s.Log.Debug("ssdp join failed", "iface", ifi.Name, "err", err)
		}
	}
	s.mu.Lock()
	if len(joined) > 0 || s.ifaces == nil {
		s.ifaces = joined
	}
	s.mu.Unlock()
}

// announce sends ssdp:alive while active (and byebye when it stops being so).
func (s *SSDP) announce(ctx context.Context, p *ipv4.PacketConn) {
	t := time.NewTicker(stateCheck)
	defer t.Stop()
	var last time.Time
	was := false
	for {
		active := s.Active()
		switch {
		case active && (!was || time.Since(last) >= announceEvery):
			s.join(p) // interfaces come and go
			s.broadcast(p, "ssdp:alive")
			last = time.Now()
		case !active && was:
			s.broadcast(p, "ssdp:byebye")
		}
		was = active
		select {
		case <-ctx.Done():
			if was {
				s.broadcast(p, "ssdp:byebye")
			}
			return
		case <-t.C:
		}
	}
}

func (s *SSDP) broadcast(p *ipv4.PacketConn, nts string) {
	ifs, _ := net.Interfaces()
	for _, ifi := range ifs {
		if ifi.Flags&net.FlagUp == 0 || ifi.Flags&net.FlagMulticast == 0 {
			continue
		}
		ip := ipv4Of(&ifi)
		if ip == nil {
			continue
		}
		addr, _ := netip.AddrFromSlice(ip.To4())
		loc := s.Location(addr)
		cm := &ipv4.ControlMessage{IfIndex: ifi.Index}
		for _, nt := range s.targets() {
			if _, err := p.WriteTo(s.notify(nt, nts, loc), cm, ssdpAddr); err != nil && s.Log != nil {
				s.Log.Debug("ssdp notify failed", "iface", ifi.Name, "err", err)
				break
			}
		}
	}
}

func ipv4Of(ifi *net.Interface) net.IP {
	addrs, _ := ifi.Addrs()
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil {
			return n.IP.To4()
		}
	}
	return nil
}

// localAddrFor picks our address facing a client: the address of the receiving
// interface on the client's subnet, else the route the system would use.
func localAddrFor(client netip.Addr, ifIndex int) netip.Addr {
	if ifIndex > 0 {
		if ifi, err := net.InterfaceByIndex(ifIndex); err == nil {
			addrs, _ := ifi.Addrs()
			var first netip.Addr
			for _, a := range addrs {
				n, ok := a.(*net.IPNet)
				if !ok || n.IP.To4() == nil {
					continue
				}
				ip, _ := netip.AddrFromSlice(n.IP.To4())
				if !first.IsValid() {
					first = ip
				}
				if n.Contains(net.IP(client.AsSlice())) {
					return ip
				}
			}
			if first.IsValid() {
				return first
			}
		}
	}
	return routeTo(client)
}

// routeTo returns the local address the system routes to ip (no packet is sent).
func routeTo(ip netip.Addr) netip.Addr {
	c, err := net.DialUDP("udp", nil, net.UDPAddrFromAddrPort(netip.AddrPortFrom(ip, 9)))
	if err != nil {
		return netip.MustParseAddr("127.0.0.1")
	}
	defer func() { _ = c.Close() }()
	if la, ok := c.LocalAddr().(*net.UDPAddr); ok {
		return la.AddrPort().Addr().Unmap()
	}
	return netip.MustParseAddr("127.0.0.1")
}

// isLocal: private, loopback or link-local senders only (LAN only, like D-033).
func isLocal(ip netip.Addr) bool {
	ip = ip.Unmap()
	return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast()
}

// limiter is a per-source token bucket plus an overall cap per second.
type limiter struct {
	mu      sync.Mutex
	buckets map[netip.Addr]*bucket
	window  time.Time
	count   int
}

type bucket struct {
	tokens float64
	at     time.Time
}

// Limits: a TV sends a burst of searches for several types at once, so each
// source gets a burst of 8 and 2 more per second; at most 40 searches per
// second are answered overall (each answer is at most five ~300-byte replies).
const (
	burst       = 8.0
	refillPerS  = 2.0
	maxOverallS = 40
)

func (l *limiter) allow(ip netip.Addr, now time.Time) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if now.Sub(l.window) >= time.Second {
		l.window, l.count = now, 0
	}
	if l.count >= maxOverallS {
		return false
	}
	if l.buckets == nil || len(l.buckets) > 4096 {
		l.buckets = map[netip.Addr]*bucket{}
	}
	b, ok := l.buckets[ip]
	if !ok {
		b = &bucket{tokens: burst, at: now}
		l.buckets[ip] = b
	}
	b.tokens = min(burst, b.tokens+now.Sub(b.at).Seconds()*refillPerS)
	b.at = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	l.count++
	return true
}
