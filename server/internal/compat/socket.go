package compat

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coder/websocket"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/auth"
)

// SocketKeepAlive is the timeout announced to clients in ForceKeepAlive: they
// answer with KeepAlive at least every half of it (public API description).
const SocketKeepAlive = 60 * time.Second

// Commands delivers the dashboard's session commands (message, stop) to clients
// with a live socket. *activity.Registry implements it.
type Commands interface {
	Watch(key string) (<-chan struct{}, func())
	Take(key string) []activity.Command
}

// socketMessage is the envelope of every socket message in both directions.
type socketMessage struct {
	MessageType string `json:"MessageType"`
	MessageID   string `json:"MessageId,omitempty"`
	Data        any    `json:"Data,omitempty"`
}

// socketToken reads the credential of a socket request. Clients pass it as the
// api_key query parameter or in the MediaBrowser headers; a plain bearer header
// is ignored here because the web session cookie is turned into one, and a
// cross-site page must not be able to open a socket with a visitor's cookie.
func socketToken(r *http.Request) string {
	c := clientInfo(r)
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") && c.token == strings.TrimPrefix(h, "Bearer ") {
		return ""
	}
	return c.token
}

func sessionKey(p auth.Principal) string {
	if p.SessionID == uuid.Nil {
		return "key:" + p.APIKeyID.String()
	}
	return p.SessionID.String()
}

// socket serves GET /socket: keepalive and the session commands Streamero has
// (a message or stop from the admin dashboard). Remote control beyond that,
// SyncPlay and live library events are not offered.
func (s *Server) socket(w http.ResponseWriter, r *http.Request) {
	p, err := s.Auth.Authenticate(r.Context(), socketToken(r))
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	// Origin is not checked: TV apps run from file:// or app origins, and the
	// credential is never ambient (see socketToken).
	conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 16)
	// The connection outlives the request's timeout middleware; it ends when
	// the client goes away, stays silent too long, or the server shuts down.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	defer func() { _ = conn.CloseNow() }()

	out := make(chan socketMessage, 8)
	in := make(chan struct{}, 1)
	go func() {
		defer cancel()
		for {
			rctx, rcancel := context.WithTimeout(ctx, 2*SocketKeepAlive)
			_, data, err := conn.Read(rctx)
			rcancel()
			if err != nil {
				return
			}
			var m socketMessage
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			if strings.EqualFold(m.MessageType, "KeepAlive") {
				select {
				case in <- struct{}{}:
				default:
				}
			}
		}
	}()

	var wake <-chan struct{}
	if s.Commands != nil {
		var stop func()
		wake, stop = s.Commands.Watch(sessionKey(p))
		defer stop()
	}
	send := func(m socketMessage) bool {
		if m.MessageID == "" {
			m.MessageID = ID(uuid.New())
		}
		b, _ := json.Marshal(m)
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}
	if !send(socketMessage{MessageType: "ForceKeepAlive", Data: int(SocketKeepAlive / time.Second)}) {
		return
	}
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case <-in:
			if !send(socketMessage{MessageType: "KeepAlive"}) {
				return
			}
		case <-wake:
			for _, c := range s.Commands.Take(sessionKey(p)) {
				select {
				case out <- s.commandMessage(ctx, c):
				default:
				}
			}
			for len(out) > 0 {
				if !send(<-out) {
					return
				}
			}
		}
	}
}

// commandMessage turns a dashboard command into the client message for it.
func (s *Server) commandMessage(ctx context.Context, c activity.Command) socketMessage {
	if c.Kind == "stop" {
		return socketMessage{MessageType: "Playstate", Data: map[string]any{"Command": "Stop"}}
	}
	return socketMessage{MessageType: "GeneralCommand", Data: map[string]any{
		"Name": "DisplayMessage",
		"Arguments": map[string]string{
			"Header": s.name(ctx), "Text": c.Text, "TimeoutMs": "10000",
		},
	}}
}
