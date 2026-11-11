package app

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
)

// syncItems creates a movie library with two films and returns their ids.
func syncItems(t *testing.T, adminC *client) (string, string) {
	t.Helper()
	movies := mediaRoot + "/movies"
	for _, name := range []string{"Alpha (2020)", "Beta (2021)"} {
		if err := os.MkdirAll(movies+"/"+name, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(movies+"/"+name+"/"+name+".mp4", []byte("0123456789"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	var lib struct {
		ID string `json:"id"`
	}
	adminC.do("POST", "/libraries", map[string]any{"name": "Films", "kind": "movies", "paths": []string{movies}}, &lib)
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mp4", VideoCodec: "h264", Width: 1920, Height: 1080, DurationMS: 6_000_000,
				Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}, nil
		}}
	if _, err := scanner.Scan(context.Background(), uuid.MustParse(lib.ID)); err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
		} `json:"items"`
	}
	adminC.do("GET", "/libraries/"+lib.ID+"/items?sort=title", nil, &page)
	if len(page.Items) != 2 {
		t.Fatalf("items: %+v", page.Items)
	}
	return page.Items[0].ID, page.Items[1].ID
}

type syncMsg struct {
	Type       string `json:"type"`
	ServerTime int64  `json:"serverTime"`
	ClientTime int64  `json:"clientTime"`
	Reason     string `json:"reason"`
	Code       string `json:"code"`
	Group      *struct {
		ID         string  `json:"id"`
		State      string  `json:"state"`
		Index      int     `json:"index"`
		PositionMs int64   `json:"positionMs"`
		AnchorAt   int64   `json:"anchorAt"`
		Speed      float64 `json:"speed"`
		Actor      string  `json:"actor"`
		SelfID     string  `json:"selfId"`
		Queue      []struct {
			ID     string `json:"id"`
			ItemID string `json:"itemId"`
			Title  string `json:"title"`
		} `json:"queue"`
		Members []struct {
			ID        string `json:"id"`
			Name      string `json:"name"`
			Ready     bool   `json:"ready"`
			Buffering bool   `json:"buffering"`
			Connected bool   `json:"connected"`
		} `json:"members"`
	} `json:"group"`
	Correction *struct {
		Mode    string  `json:"mode"`
		Rate    float64 `json:"rate"`
		DriftMs int64   `json:"driftMs"`
	} `json:"correction"`
}

// syncPeer is a simulated player on the watch-together socket.
type syncPeer struct {
	t    *testing.T
	name string
	conn *websocket.Conn
	last *syncMsg // last group state
}

func dialSync(t *testing.T, srv string, token string, hdr http.Header) (*websocket.Conn, *http.Response, error) {
	if hdr == nil {
		hdr = http.Header{}
	}
	if token != "" {
		hdr.Set("Authorization", "Bearer "+token)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv, "http")+"/api/v1/syncplay/socket", &websocket.DialOptions{HTTPHeader: hdr})
}

func newSyncPeer(t *testing.T, srv, name, token string) *syncPeer {
	t.Helper()
	conn, _, err := dialSync(t, srv, token, nil)
	if err != nil {
		t.Fatalf("%s socket: %v", name, err)
	}
	t.Cleanup(func() { _ = conn.CloseNow() })
	p := &syncPeer{t: t, name: name, conn: conn}
	if m := p.next(func(m syncMsg) bool { return true }); m.Type != "hello" || m.ServerTime == 0 {
		t.Fatalf("%s first message: %+v", name, m)
	}
	return p
}

func (p *syncPeer) send(m map[string]any) {
	p.t.Helper()
	b, _ := json.Marshal(m)
	if err := p.conn.Write(context.Background(), websocket.MessageText, b); err != nil {
		p.t.Fatalf("%s write: %v", p.name, err)
	}
}

// next reads messages until one matches.
func (p *syncPeer) next(match func(syncMsg) bool) syncMsg {
	p.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := p.conn.Read(ctx)
		if err != nil {
			p.t.Fatalf("%s: no matching message: %v (last state %+v)", p.name, err, p.last)
		}
		var m syncMsg
		if err := json.Unmarshal(b, &m); err != nil {
			p.t.Fatal(err)
		}
		if m.Type == "state" && m.Group != nil {
			p.last = &m
		}
		if match(m) {
			return m
		}
	}
}

// state waits for a group state that matches.
func (p *syncPeer) state(match func(syncMsg) bool) syncMsg {
	p.t.Helper()
	return p.next(func(m syncMsg) bool { return m.Type == "state" && m.Group != nil && match(m) })
}

func (p *syncPeer) entry() string { return p.last.Group.Queue[p.last.Group.Index].ID }

// TestSyncPlayIntegration runs two simulated players through a watch-together
// session over the real socket: create, join, synchronised start, pause, seek,
// drift correction, buffering wait, reconnect, a new queue, leave, permissions,
// library access and rate limiting.
func TestSyncPlayIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	alpha, beta := syncItems(t, adminC)

	users := map[string]map[string]any{}
	for _, name := range []string{"ben", "nosync", "nolib"} {
		var u struct {
			ID    string         `json:"id"`
			Perms map[string]any `json:"permissions"`
		}
		adminC.do("POST", "/users", map[string]any{"username": name, "password": name + "-pass-1"}, &u)
		users[name] = u.Perms
		switch name {
		case "nosync":
			u.Perms["syncPlay"] = false
		case "nolib":
			u.Perms["allLibraries"] = false
		}
		if s := adminC.do("PATCH", "/users/"+u.ID, map[string]any{"permissions": u.Perms}, nil); s != 200 {
			t.Fatalf("permissions of %s: %d", name, s)
		}
	}
	login := func(name string) *client {
		c := newClient(t, srv)
		var res authResult
		if s := c.do("POST", "/auth/login", map[string]any{"username": name, "password": name + "-pass-1",
			"device": map[string]any{"name": name + " phone"}}, &res); s != 200 {
			t.Fatalf("login %s: %d", name, s)
		}
		c.token = res.Token
		return c
	}
	benC, noSync, noLib := login("ben"), login("nosync"), login("nolib")

	// Without the permission: no groups, no socket.
	var e apiError
	if s := noSync.do("GET", "/syncplay/groups", nil, &e); s != 403 || e.Code != "forbidden" {
		t.Fatalf("list without permission: %d %+v", s, e)
	}
	if _, resp, err := dialSync(t, srv.URL, noSync.token, nil); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("socket without permission: %v %v", resp, err)
	}
	if _, resp, err := dialSync(t, srv.URL, "", nil); err == nil || resp == nil || resp.StatusCode != 401 {
		t.Fatalf("anonymous socket: %v %v", resp, err)
	}

	// The admin starts a group with Alpha at 1:00.
	var g struct {
		ID     string `json:"id"`
		State  string `json:"state"`
		SelfID string `json:"selfId"`
		Queue  []struct {
			ItemID string `json:"itemId"`
			Title  string `json:"title"`
		} `json:"queue"`
	}
	if s := adminC.do("POST", "/syncplay/groups", map[string]any{"itemIds": []string{alpha}, "startMs": 60_000}, &g); s != 200 ||
		g.State != "waiting" || g.SelfID == "" || len(g.Queue) != 1 || g.Queue[0].ItemID != alpha || g.Queue[0].Title != "Alpha" {
		t.Fatalf("create: %d %+v", s, g)
	}
	// A user without library access neither sees nor joins it.
	var list []map[string]any
	if s := noLib.do("GET", "/syncplay/groups", nil, &list); s != 200 || len(list) != 0 {
		t.Fatalf("list without library access: %d %v", s, list)
	}
	if s := noLib.do("POST", "/syncplay/groups/"+g.ID+"/join", nil, &e); s != 403 {
		t.Fatalf("join without library access: %d", s)
	}
	if s := benC.do("GET", "/syncplay/groups", nil, &list); s != 200 || len(list) != 1 {
		t.Fatalf("ben's list: %d %v", s, list)
	}
	if s := benC.do("GET", "/syncplay/me", nil, nil); s != 204 {
		t.Fatalf("ben not in a group yet: %d", s)
	}
	if s := benC.do("POST", "/syncplay/groups/"+g.ID+"/join", nil, &g); s != 200 || g.SelfID == "" {
		t.Fatalf("join: %d %+v", s, g)
	}

	admin := newSyncPeer(t, srv.URL, "admin", adminC.token)
	ben := newSyncPeer(t, srv.URL, "ben", benC.token)
	admin.state(func(m syncMsg) bool { return len(m.Group.Members) == 2 && m.Group.Members[1].Connected })
	ben.state(func(m syncMsg) bool { return m.Group.SelfID == g.SelfID })

	// Clock sync.
	ben.send(map[string]any{"type": "ping", "clientTime": 12345})
	if m := ben.next(func(m syncMsg) bool { return m.Type == "pong" }); m.ClientTime != 12345 || m.ServerTime == 0 {
		t.Fatalf("pong: %+v", m)
	}

	// Both load Alpha: once both are ready the start is scheduled for everyone.
	admin.send(map[string]any{"type": "ready", "entryId": admin.entry(), "positionMs": 60_000})
	ben.state(func(m syncMsg) bool {
		return m.Group.State == "waiting" && m.Group.Members[0].Ready && !m.Group.Members[1].Ready
	})
	ben.send(map[string]any{"type": "ready", "entryId": ben.entry(), "positionMs": 60_000})
	start := admin.state(func(m syncMsg) bool { return m.Group.State == "playing" })
	if start.Group.PositionMs != 60_000 || start.Group.AnchorAt <= start.ServerTime {
		t.Fatalf("scheduled start: %+v", start.Group)
	}
	ben.state(func(m syncMsg) bool { return m.Group.State == "playing" && m.Group.AnchorAt == start.Group.AnchorAt })

	// Pause from one member pauses everyone.
	admin.send(map[string]any{"type": "pause"})
	if m := ben.state(func(m syncMsg) bool { return m.Group.State == "paused" }); m.Group.Actor != "Admin" || m.Group.PositionMs < 60_000 {
		t.Fatalf("pause: %+v", m.Group)
	}
	// Seek: everyone gets ready at the new position, then it plays again.
	ben.send(map[string]any{"type": "seek", "positionMs": 120_000})
	admin.state(func(m syncMsg) bool { return m.Group.State == "paused" && m.Group.PositionMs == 120_000 })
	ben.send(map[string]any{"type": "play"})
	admin.state(func(m syncMsg) bool { return m.Group.State == "waiting" })
	admin.send(map[string]any{"type": "ready", "entryId": admin.entry()})
	ben.send(map[string]any{"type": "ready", "entryId": ben.entry()})
	playing := ben.state(func(m syncMsg) bool { return m.Group.State == "playing" && m.Group.PositionMs == 120_000 })
	admin.state(func(m syncMsg) bool { return m.Group.State == "playing" })

	// Drift: Ben reports 800 ms behind the shared position and is told to speed
	// up; the admin, in sync, is left alone.
	time.Sleep(time.Until(time.UnixMilli(playing.Group.AnchorAt)) + 1200*time.Millisecond) // past the start and the settle time
	now := time.Now().UnixMilli()
	shared := playing.Group.PositionMs + (now - playing.Group.AnchorAt)
	ben.send(map[string]any{"type": "position", "entryId": ben.entry(), "positionMs": shared - 800, "at": now, "playing": true})
	admin.send(map[string]any{"type": "position", "entryId": admin.entry(), "positionMs": shared, "at": now, "playing": true})
	if m := ben.next(func(m syncMsg) bool { return m.Type == "correction" }); m.Correction.Mode != "rate" || m.Correction.Rate <= 1 ||
		m.Correction.DriftMs > -700 || m.Correction.DriftMs < -900 {
		t.Fatalf("correction: %+v", m.Correction)
	}

	// The admin buffers: Ben waits for them.
	admin.send(map[string]any{"type": "buffering", "entryId": admin.entry()})
	if m := ben.state(func(m syncMsg) bool { return m.Group.State == "waiting" }); !m.Group.Members[0].Buffering || m.Group.Actor != "Admin" {
		t.Fatalf("waiting for admin: %+v", m.Group)
	}
	// Ben's connection drops: membership stays, and he does not hold anyone up.
	_ = ben.conn.CloseNow()
	admin.state(func(m syncMsg) bool { return !m.Group.Members[1].Connected })
	admin.send(map[string]any{"type": "ready", "entryId": admin.entry()})
	admin.state(func(m syncMsg) bool { return m.Group.State == "playing" })
	ben = newSyncPeer(t, srv.URL, "ben", benC.token)
	if m := ben.state(func(m syncMsg) bool { return true }); m.Group.ID != g.ID || m.Group.SelfID != g.SelfID || m.Group.State != "playing" {
		t.Fatalf("after reconnect: %+v", m.Group)
	}

	// A new queue for everyone.
	ben.send(map[string]any{"type": "setQueue", "itemIds": []string{beta, alpha}})
	if m := admin.state(func(m syncMsg) bool { return len(m.Group.Queue) == 2 }); m.Group.Queue[0].ItemID != beta || m.Group.State != "waiting" {
		t.Fatalf("new queue: %+v", m.Group)
	}

	// Leaving stops nothing for the others.
	ben.state(func(m syncMsg) bool { return len(m.Group.Queue) == 2 })
	ben.send(map[string]any{"type": "ready", "entryId": ben.entry()})
	admin.send(map[string]any{"type": "ready", "entryId": admin.entry()})
	admin.state(func(m syncMsg) bool { return m.Group.State == "playing" })
	if s := benC.do("POST", "/syncplay/leave", nil, nil); s != 204 {
		t.Fatalf("leave: %d", s)
	}
	if m := ben.next(func(m syncMsg) bool { return m.Type == "state" && m.Group == nil }); m.Reason != "left" {
		t.Fatalf("leaver told: %+v", m)
	}
	if m := admin.state(func(m syncMsg) bool { return len(m.Group.Members) == 1 }); m.Group.State != "playing" {
		t.Fatalf("after ben left: %+v", m.Group)
	}

	// Commands outside a group and floods are refused over the socket.
	ben.send(map[string]any{"type": "pause"})
	if m := ben.next(func(m syncMsg) bool { return m.Type == "error" }); m.Code != "not_in_group" {
		t.Fatalf("outsider command: %+v", m)
	}
	for range 40 {
		admin.send(map[string]any{"type": "speed", "speed": 1.25})
	}
	if m := admin.next(func(m syncMsg) bool { return m.Type == "error" }); m.Code != "rate_limited" {
		t.Fatalf("flood: %+v", m)
	}
}

// TestSyncPlaySocketOrigin: the web client authenticates the socket with its
// cookie, so a page on another origin must not be able to open it.
func TestSyncPlaySocketOrigin(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	web := newClient(t, srv)
	var res authResult
	web.do("POST", "/auth/login", map[string]any{"username": "Admin", "password": "admin-pass-1", "useCookie": true}, &res)
	u, _ := http.NewRequest("GET", srv.URL, nil)
	var cookie string
	for _, c := range web.http.Jar.Cookies(u.URL) {
		cookie += c.Name + "=" + c.Value
	}
	if cookie == "" {
		t.Fatal("no session cookie")
	}
	host := strings.TrimPrefix(srv.URL, "http://")
	conn, _, err := dialSync(t, srv.URL, "", http.Header{"Cookie": {cookie}, "Origin": {"http://" + host}})
	if err != nil {
		t.Fatalf("same-origin socket with the cookie: %v", err)
	}
	_ = conn.CloseNow()
	if _, resp, err := dialSync(t, srv.URL, "", http.Header{"Cookie": {cookie}, "Origin": {"https://evil.example"}}); err == nil || resp == nil || resp.StatusCode != 403 {
		t.Fatalf("cross-origin socket: %v %v", resp, err)
	}
}
