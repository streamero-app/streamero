package app

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// compatSocket reads Jellyfin-style socket messages of a media client.
type compatSocket struct {
	t    *testing.T
	conn *websocket.Conn
}

func (c *compatSocket) next(match func(typ string, data map[string]any) bool) map[string]any {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for {
		_, b, err := c.conn.Read(ctx)
		if err != nil {
			c.t.Fatalf("media client socket: %v", err)
		}
		var m struct {
			MessageType string
			Data        json.RawMessage
		}
		if err := json.Unmarshal(b, &m); err != nil {
			c.t.Fatal(err)
		}
		var data map[string]any
		_ = json.Unmarshal(m.Data, &data)
		if match(m.MessageType, data) {
			return data
		}
	}
}

func (c *compatSocket) update(kind string) map[string]any {
	c.t.Helper()
	return c.next(func(typ string, d map[string]any) bool { return typ == "SyncPlayGroupUpdate" && d["Type"] == kind })
}

func (c *compatSocket) command(cmd string) map[string]any {
	c.t.Helper()
	return c.next(func(typ string, d map[string]any) bool { return typ == "SyncPlayCommand" && d["Command"] == cmd })
}

// TestSyncPlayCompatIntegration: a TV app (compat API + /socket) joins a group
// started by a Streamero client and follows its start, pause and seek.
func TestSyncPlayCompatIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	alpha, _ := syncItems(t, adminC)
	adminC.do("POST", "/users", map[string]any{"username": "tv", "password": "tv-pass-123"}, nil)

	var g struct {
		ID string `json:"id"`
	}
	if s := adminC.do("POST", "/syncplay/groups", map[string]any{"name": "Movie night", "itemIds": []string{alpha}}, &g); s != 200 {
		t.Fatalf("create: %d", s)
	}
	admin := newSyncPeer(t, srv.URL, "admin", adminC.token)
	admin.state(func(syncMsg) bool { return true })

	tv := &compatCaller{t: t, base: srv.URL}
	var login struct{ AccessToken string }
	if s := tv.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "tv", "Pw": "tv-pass-123"}, &login); s != 200 {
		t.Fatalf("tv login: %d", s)
	}
	tv.token = login.AccessToken
	var me struct{ Policy map[string]any }
	if tv.call("GET", "/Users/Me", nil, &me); me.Policy["SyncPlayAccess"] != "CreateAndJoinGroups" {
		t.Fatalf("policy: %v", me.Policy["SyncPlayAccess"])
	}
	var utc map[string]string
	if s := tv.call("GET", "/GetUtcTime", nil, &utc); s != 200 || utc["RequestReceptionTime"] == "" {
		t.Fatalf("utc time: %d %v", s, utc)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http")+"/socket?api_key="+tv.token+"&deviceId=tv-0001", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.CloseNow() }()
	sock := &compatSocket{t: t, conn: conn}

	var groups []map[string]any
	if s := tv.call("GET", "/SyncPlay/List", nil, &groups); s != 200 || len(groups) != 1 || groups[0]["GroupName"] != "Movie night" ||
		!hex32.MatchString(groups[0]["GroupId"].(string)) {
		t.Fatalf("list: %d %v", s, groups)
	}
	if s := tv.call("POST", "/SyncPlay/Join", map[string]any{"GroupId": groups[0]["GroupId"]}, nil); s != 204 {
		t.Fatalf("join: %d", s)
	}
	if d := sock.update("GroupJoined"); d["GroupId"] != groups[0]["GroupId"] {
		t.Fatalf("joined: %v", d)
	}
	queue := sock.update("PlayQueue")["Data"].(map[string]any)
	playlist := queue["Playlist"].([]any)
	entry := playlist[0].(map[string]any)
	if len(playlist) != 1 || entry["ItemId"] != strings.ReplaceAll(alpha, "-", "") {
		t.Fatalf("queue: %v", queue)
	}
	admin.state(func(m syncMsg) bool { return len(m.Group.Members) == 2 && m.Group.Members[1].Connected })

	// Both ready: the TV gets an Unpause timed at the shared start.
	if s := tv.call("POST", "/SyncPlay/Ready", map[string]any{"When": time.Now().UTC().Format(time.RFC3339Nano), "PositionTicks": 0,
		"IsPlaying": true, "PlaylistItemId": entry["PlaylistItemId"]}, nil); s != 204 {
		t.Fatalf("ready: %d", s)
	}
	admin.send(map[string]any{"type": "ready", "entryId": admin.entry()})
	start := sock.command("Unpause")
	when, err := time.Parse(time.RFC3339Nano, start["When"].(string))
	if err != nil || start["PlaylistItemId"] != entry["PlaylistItemId"] || start["PositionTicks"] != float64(0) {
		t.Fatalf("unpause: %v %v", start, err)
	}
	playing := admin.state(func(m syncMsg) bool { return m.Group.State == "playing" })
	if when.UnixMilli() != playing.Group.AnchorAt {
		t.Fatalf("TV start %v, group anchor %d", when, playing.Group.AnchorAt)
	}

	// The Streamero client pauses, the TV pauses; the TV seeks, the Streamero client follows.
	admin.send(map[string]any{"type": "pause"})
	sock.command("Pause")
	if s := tv.call("POST", "/SyncPlay/Seek", map[string]any{"PositionTicks": int64(90_000) * 10_000}, nil); s != 204 {
		t.Fatalf("seek: %d", s)
	}
	admin.state(func(m syncMsg) bool { return m.Group.PositionMs == 90_000 && m.Group.Actor == "tv" })

	// The TV leaves; the group stays with the Streamero client.
	if s := tv.call("POST", "/SyncPlay/Leave", nil, nil); s != 204 {
		t.Fatalf("leave: %d", s)
	}
	sock.update("GroupLeft")
	admin.state(func(m syncMsg) bool { return len(m.Group.Members) == 1 })
}
