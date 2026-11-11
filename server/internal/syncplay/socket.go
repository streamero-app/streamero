package syncplay

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/coder/websocket"
)

// ReadTimeout closes a socket whose client has been silent this long; clients
// ping every few seconds to keep their clock offset fresh.
const ReadTimeout = 75 * time.Second

// ServerMessage is a socket message to a client (the SyncServerMessage schema).
type ServerMessage struct {
	Type       string      `json:"type"`
	ServerTime int64       `json:"serverTime"`
	ClientTime int64       `json:"clientTime,omitempty"`
	Group      *Snapshot   `json:"group,omitempty"`
	Reason     string      `json:"reason,omitempty"`
	Correction *Correction `json:"correction,omitempty"`
	Code       string      `json:"code,omitempty"`
	Message    string      `json:"message,omitempty"`
}

// ErrorCode maps a group error to the code clients see.
func ErrorCode(err error) string {
	switch {
	case errors.Is(err, ErrRateLimited):
		return "rate_limited"
	case errors.Is(err, ErrNotInGroup):
		return "not_in_group"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrNotFound):
		return "not_found"
	case errors.Is(err, ErrTooManyGroups):
		return "too_many_groups"
	}
	return "invalid"
}

// ServeSocket runs the watch-together socket of an authenticated member. The
// browser's Origin must match the host (the web client authenticates with its
// cookie); native apps send no Origin and authenticate with a bearer token.
func (s *Service) ServeSocket(w http.ResponseWriter, r *http.Request, id Identity) {
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(1 << 16)
	// The connection outlives the request timeout middleware.
	ctx, cancel := context.WithCancel(context.WithoutCancel(r.Context()))
	defer cancel()
	defer func() { _ = conn.CloseNow() }()

	events, unsubscribe := s.Hub.Subscribe(id.Key)
	defer unsubscribe()
	out := make(chan ServerMessage, 16)
	now := func() int64 { return s.Hub.now().UnixMilli() }
	go func() {
		defer cancel()
		for {
			rctx, rcancel := context.WithTimeout(ctx, ReadTimeout)
			_, data, err := conn.Read(rctx)
			rcancel()
			if err != nil {
				return
			}
			var m Message
			if json.Unmarshal(data, &m) != nil {
				continue
			}
			var reply *ServerMessage
			if m.Type == "ping" {
				reply = &ServerMessage{Type: "pong", ClientTime: m.ClientTime, ServerTime: now()}
			} else if err := s.Handle(ctx, id, m); err != nil {
				reply = &ServerMessage{Type: "error", Code: ErrorCode(err), Message: err.Error(), ServerTime: now()}
			}
			if reply != nil {
				select {
				case out <- *reply:
				default:
				}
			}
		}
	}()

	write := func(m ServerMessage) bool {
		b, _ := json.Marshal(m)
		wctx, wcancel := context.WithTimeout(ctx, 10*time.Second)
		defer wcancel()
		return conn.Write(wctx, websocket.MessageText, b) == nil
	}
	if !write(ServerMessage{Type: "hello", ServerTime: now()}) {
		return
	}
	if g, ok := s.Hub.GroupOf(id.Key); ok {
		// Subscribe already queued the state when it changed; sending it again
		// is harmless and covers a second connection of the same session.
		if !write(ServerMessage{Type: "state", Group: &g, ServerTime: g.ServerTime}) {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			_ = conn.Close(websocket.StatusNormalClosure, "")
			return
		case m := <-out:
			if !write(m) {
				return
			}
		case e := <-events:
			if !write(EventMessage(e)) {
				return
			}
		}
	}
}

// EventMessage turns a hub event into the socket message for it.
func EventMessage(e Event) ServerMessage {
	at := e.At.UnixMilli()
	switch {
	case e.Correction != nil:
		return ServerMessage{Type: "correction", Correction: e.Correction, ServerTime: at}
	case e.Group != nil:
		return ServerMessage{Type: "state", Group: e.Group, ServerTime: at}
	}
	return ServerMessage{Type: "state", Reason: e.Reason, ServerTime: at}
}
