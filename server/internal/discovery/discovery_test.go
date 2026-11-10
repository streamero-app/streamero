package discovery

import (
	"context"
	"encoding/json"
	"net"
	"net/netip"
	"strconv"
	"strings"
	"testing"
	"time"
)

func newResponder() *Responder {
	return &Responder{HTTPPort: 8787, Info: func(context.Context) (string, string) {
		return "0123456789abcdef0123456789abcdef", "Kino"
	}}
}

func TestReplyToProbe(t *testing.T) {
	ctx := context.Background()
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(ctx)
	done := make(chan error, 1)
	go func() { done <- newResponder().Serve(ctx, pc) }()

	c, err := net.Dial("udp", pc.LocalAddr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = c.Close() }()
	read := func() []byte {
		_ = c.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
		buf := make([]byte, 1024)
		n, err := c.Read(buf)
		if err != nil {
			return nil
		}
		return buf[:n]
	}
	// Junk gets no answer.
	_, _ = c.Write([]byte("hello"))
	if b := read(); b != nil {
		t.Fatalf("answered junk: %s", b)
	}
	_, _ = c.Write([]byte("Who is JellyfinServer?"))
	b := read()
	if b == nil {
		t.Fatal("no reply to the probe")
	}
	var info map[string]any
	if err := json.Unmarshal(b, &info); err != nil {
		t.Fatal(err)
	}
	if info["Id"] != "0123456789abcdef0123456789abcdef" || info["Name"] != "Kino" ||
		info["Address"] != "http://127.0.0.1:8787" || info["EndpointAddress"] != nil {
		t.Fatalf("reply %s", b)
	}
	if _, ok := info["EndpointAddress"]; !ok {
		t.Fatal("EndpointAddress missing")
	}
	cancel()
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestReplyRules(t *testing.T) {
	ctx := context.Background()
	now := time.Unix(1000, 0)
	r := newResponder()
	r.Now = func() time.Time { return now }
	r.URL = "http://media.lan:8787"
	from := func(s string) net.Addr { return net.UDPAddrFromAddrPort(mustAddrPort(t, s)) }

	for _, msg := range []string{"who is jellyfinserver", "WHO IS JELLYFINSERVER?\n", "Who is JellyfinServer?\x00"} {
		now = now.Add(time.Second)
		if r.Reply(ctx, []byte(msg), from("192.168.1.20:5000")) == nil {
			t.Errorf("no reply to %q", msg)
		}
	}
	now = now.Add(time.Second)
	if b := r.Reply(ctx, []byte("Who is JellyfinServer?"), from("10.0.0.5:5000")); !strings.Contains(string(b), `"Address":"http://media.lan:8787"`) {
		t.Errorf("configured URL not announced: %s", b)
	}
	// Senders outside the local network never get an answer.
	for _, a := range []string{"8.8.8.8:5000", "[2001:db8::1]:5000"} {
		if r.Reply(ctx, []byte("Who is JellyfinServer?"), from(a)) != nil {
			t.Errorf("answered %s", a)
		}
	}
	if r.Reply(ctx, []byte("Who is JellyfinServer?"), from("[fe80::1]:5000")) == nil {
		t.Error("no reply to link-local IPv6")
	}
	// Rate limit per source.
	now = now.Add(time.Second)
	if r.Reply(ctx, []byte("Who is JellyfinServer?"), from("192.168.1.30:1")) == nil {
		t.Fatal("first probe not answered")
	}
	if r.Reply(ctx, []byte("Who is JellyfinServer?"), from("192.168.1.30:2")) != nil {
		t.Error("second probe in the same instant answered")
	}
	// Global limit.
	now = now.Add(time.Second)
	answered := 0
	for i := 0; i < 100; i++ {
		if r.Reply(ctx, []byte("Who is JellyfinServer?"), from("192.168.2."+itoa(i+1)+":1")) != nil {
			answered++
		}
	}
	if answered != maxPerSecond {
		t.Errorf("answered %d in one second, want %d", answered, maxPerSecond)
	}
}

func mustAddrPort(t *testing.T, s string) netip.AddrPort {
	t.Helper()
	ap, err := netip.ParseAddrPort(s)
	if err != nil {
		t.Fatal(err)
	}
	return ap
}

func itoa(i int) string { return strconv.Itoa(i) }

func TestPortOf(t *testing.T) {
	if PortOf(":8787") != 8787 || PortOf("0.0.0.0:9000") != 9000 || PortOf("bad") != 0 {
		t.Fatal("PortOf")
	}
}
