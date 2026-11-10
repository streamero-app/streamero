package app

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/compat"
)

// TestCompatRootAndSocketIntegration signs a TV client in on the plain server
// address (no /compat), browses, opens /socket the way clients do, keeps it alive
// and receives the admin dashboard's message and stop commands.
func TestCompatRootAndSocketIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	testState.Compat().Commands = testNowPlaying
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	adminC.do("POST", "/users", map[string]any{"username": "tv", "password": "tv-pass-123"}, nil)

	c := &compatCaller{t: t, base: srv.URL}
	var pub map[string]any
	if s := c.call("GET", "/System/Info/Public", nil, &pub); s != 200 || pub["ProductName"] != compat.ProductName || pub["ServerName"] != "Kino" {
		t.Fatalf("public info at root: %d %v", s, pub)
	}
	var login struct {
		AccessToken string
		SessionInfo struct {
			ID string `json:"Id"`
		}
	}
	if s := c.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "tv", "Pw": "tv-pass-123"}, &login); s != 200 || login.AccessToken == "" {
		t.Fatalf("login at root: %d", s)
	}
	c.token = login.AccessToken
	var views compatList
	if s := c.call("GET", "/UserViews", nil, &views); s != 200 {
		t.Fatalf("views at root: %d", s)
	}
	var me map[string]any
	if s := c.call("GET", "/users/me", nil, &me); s != 200 || me["Name"] != "tv" {
		t.Fatalf("me at root (lower case): %d %v", s, me)
	}
	// Without a token the API refuses, at the root as under /compat.
	anon := &compatCaller{t: t, base: srv.URL}
	if s := anon.call("GET", "/Items", nil, nil); s != 401 {
		t.Fatalf("anonymous items at root: %d", s)
	}

	ws := "ws" + strings.TrimPrefix(srv.URL, "http")
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	// Socket auth: no token, a wrong token, or only a bearer header (what the
	// web session cookie becomes) are refused before the upgrade.
	for _, tc := range []struct {
		url string
		hdr http.Header
	}{
		{ws + "/socket?deviceId=tv-0001", nil},
		{ws + "/socket?api_key=wrong&deviceId=tv-0001", nil},
		{ws + "/socket", http.Header{"Authorization": {"Bearer " + login.AccessToken}}},
	} {
		conn, resp, err := websocket.Dial(ctx, tc.url, &websocket.DialOptions{HTTPHeader: tc.hdr})
		if err == nil {
			_ = conn.CloseNow()
			t.Fatalf("%s: socket opened without valid credentials", tc.url)
		}
		if resp == nil || resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("%s: %v %v", tc.url, resp, err)
		}
	}

	read := func(conn *websocket.Conn) map[string]any {
		t.Helper()
		rctx, rcancel := context.WithTimeout(ctx, 5*time.Second)
		defer rcancel()
		_, b, err := conn.Read(rctx)
		if err != nil {
			t.Fatalf("socket read: %v", err)
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatal(err)
		}
		if id, _ := m["MessageId"].(string); !hex32.MatchString(id) {
			t.Fatalf("message without id: %s", b)
		}
		return m
	}

	// Under /compat with the MediaBrowser header, too.
	alt, _, err := websocket.Dial(ctx, ws+"/compat/socket", &websocket.DialOptions{HTTPHeader: http.Header{
		"X-Emby-Authorization": {`MediaBrowser Client="Living Room TV", Device="Television", DeviceId="tv-0001", Version="1.2.3", Token="` + login.AccessToken + `"`},
	}})
	if err != nil {
		t.Fatalf("socket under /compat: %v", err)
	}
	if m := read(alt); m["MessageType"] != "ForceKeepAlive" {
		t.Fatalf("first message under /compat: %v", m)
	}
	_ = alt.Close(websocket.StatusNormalClosure, "")

	conn, _, err := websocket.Dial(ctx, ws+"/socket?api_key="+login.AccessToken+"&deviceId=tv-0001", nil)
	if err != nil {
		t.Fatalf("socket: %v", err)
	}
	defer func() { _ = conn.CloseNow() }()
	if m := read(conn); m["MessageType"] != "ForceKeepAlive" || m["Data"] != float64(60) {
		t.Fatalf("first message: %v", m)
	}
	if err := conn.Write(ctx, websocket.MessageText, []byte(`{"MessageType":"KeepAlive"}`)); err != nil {
		t.Fatal(err)
	}
	if m := read(conn); m["MessageType"] != "KeepAlive" {
		t.Fatalf("keepalive answer: %v", m)
	}

	// The client is playing something; the admin sends it a message, then stops it.
	sid, err := compat.ParseID(login.SessionInfo.ID)
	if err != nil {
		t.Fatal(err)
	}
	testNowPlaying.Start(activity.Session{Key: sid.String(), User: "tv", Device: "Television", Title: "Film"})
	if s := adminC.do("POST", "/sessions/"+sid.String()+"/command", map[string]any{"kind": "message", "text": "Dinner is ready"}, nil); s != 204 {
		t.Fatalf("message command: %d", s)
	}
	m := read(conn)
	data, _ := m["Data"].(map[string]any)
	args, _ := data["Arguments"].(map[string]any)
	if m["MessageType"] != "GeneralCommand" || data["Name"] != "DisplayMessage" || args["Text"] != "Dinner is ready" ||
		args["Header"] != "Kino" || args["TimeoutMs"] == nil {
		t.Fatalf("message push: %v", m)
	}
	if s := adminC.do("POST", "/sessions/"+sid.String()+"/command", map[string]any{"kind": "stop"}, nil); s != 204 {
		t.Fatalf("stop command: %d", s)
	}
	m = read(conn)
	if data, _ := m["Data"].(map[string]any); m["MessageType"] != "Playstate" || data["Command"] != "Stop" {
		t.Fatalf("stop push: %v", m)
	}
	// Pushed commands are consumed (not delivered twice through polling).
	if left := testNowPlaying.Take(sid.String()); len(left) != 0 {
		t.Fatalf("commands left in the queue: %+v", left)
	}
	_ = conn.Close(websocket.StatusNormalClosure, "")
}
