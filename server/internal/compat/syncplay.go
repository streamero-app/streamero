package compat

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/syncplay"
)

// Watch-together for media clients (D-036): the SyncPlay endpoints of the
// public API description map onto Streamero's groups, and /socket carries the
// group updates and playback commands. Streamero's own clients and media
// clients can share a group.

// ticksPerMS converts the API's 100 ns ticks.
const ticksPerMS = 10_000

func (s *Server) syncRoutes(r chi.Router) {
	r.Get("/syncplay/list", s.syncList)
	r.Post("/syncplay/new", s.syncNew)
	r.Post("/syncplay/join", s.syncJoin)
	r.Post("/syncplay/leave", s.syncLeave)
	r.Get("/syncplay/{groupId}", s.syncGet)
	r.Post("/syncplay/pause", s.syncCommand("pause"))
	r.Post("/syncplay/unpause", s.syncCommand("play"))
	r.Post("/syncplay/stop", s.syncCommand("stop"))
	r.Post("/syncplay/seek", s.syncCommand("seek"))
	r.Post("/syncplay/setplaylistitem", s.syncCommand("jump"))
	r.Post("/syncplay/nextitem", s.syncCommand("next"))
	r.Post("/syncplay/previousitem", s.syncCommand("previous"))
	r.Post("/syncplay/setnewqueue", s.syncCommand("setQueue"))
	r.Post("/syncplay/queue", s.syncCommand("queue"))
	r.Post("/syncplay/buffering", s.syncReport("buffering"))
	r.Post("/syncplay/ready", s.syncReport("ready"))
	// Accepted without effect: ping (Streamero measures nothing from it),
	// ignore-wait (the server's wait timeout covers it), repeat/shuffle and
	// queue editing beyond append (not offered).
	for _, p := range []string{"/syncplay/ping", "/syncplay/setignorewait", "/syncplay/setrepeatmode", "/syncplay/setshufflemode",
		"/syncplay/moveplaylistitem", "/syncplay/removefromplaylist"} {
		r.Post(p, accepted)
	}
}

// utcTime serves GET /GetUtcTime, which clients use to estimate clock offset.
func utcTime(w http.ResponseWriter, _ *http.Request) {
	recv := time.Now().UTC()
	writeJSON(w, map[string]string{"RequestReceptionTime": recv.Format(time.RFC3339Nano),
		"ResponseTransmissionTime": time.Now().UTC().Format(time.RFC3339Nano)})
}

func (s *Server) syncIdentity(r *http.Request) syncplay.Identity {
	p := principal(r)
	var id syncplay.Identity
	if s.SyncIdentity != nil {
		id = s.SyncIdentity(r.Context(), p)
	} else {
		id = syncplay.Identity{Key: sessionKey(p), UserID: p.User.ID, Name: p.User.Username, Principal: p}
	}
	if id.Device == "" {
		id.Device = clientInfo(r).device
	}
	id.Compat = true
	return id
}

// syncAllowed answers 403 (or 503 before start) when the caller cannot use groups.
func (s *Server) syncAllowed(w http.ResponseWriter, r *http.Request) bool {
	if s.SyncPlay == nil {
		http.Error(w, "not available", http.StatusServiceUnavailable)
		return false
	}
	if !syncplay.Allowed(principal(r)) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return false
	}
	return true
}

func syncStatus(w http.ResponseWriter, err error) {
	switch {
	case err == nil, errors.Is(err, syncplay.ErrNotInGroup), errors.Is(err, syncplay.ErrInvalid):
		// The reference API answers group actions with 204 and reports
		// problems through group updates; a stale action is simply ignored.
		w.WriteHeader(http.StatusNoContent)
	case errors.Is(err, syncplay.ErrRateLimited):
		http.Error(w, "too many requests", http.StatusTooManyRequests)
	case errors.Is(err, syncplay.ErrForbidden):
		http.Error(w, "forbidden", http.StatusForbidden)
	case errors.Is(err, syncplay.ErrNotFound):
		http.Error(w, "not found", http.StatusNotFound)
	default:
		http.Error(w, "error", http.StatusServiceUnavailable)
	}
}

func readBody(r *http.Request, v any) {
	_ = json.NewDecoder(io.LimitReader(r.Body, 1<<16)).Decode(v)
}

func groupState(st syncplay.State) string {
	switch st {
	case syncplay.Waiting:
		return "Waiting"
	case syncplay.Paused:
		return "Paused"
	case syncplay.Playing:
		return "Playing"
	}
	return "Idle"
}

func groupInfo(g syncplay.Snapshot) map[string]any {
	names := make([]string, 0, len(g.Members))
	for _, m := range g.Members {
		names = append(names, m.Name)
	}
	return map[string]any{"GroupId": ID(g.ID), "GroupName": g.Name, "State": groupState(g.State), "Participants": names,
		"LastUpdatedAt": time.UnixMilli(g.ServerTime).UTC().Format(time.RFC3339Nano)}
}

func (s *Server) syncList(w http.ResponseWriter, r *http.Request) {
	if !s.syncAllowed(w, r) {
		return
	}
	groups, err := s.SyncPlay.List(r.Context(), principal(r))
	if err != nil {
		syncStatus(w, err)
		return
	}
	out := make([]map[string]any, 0, len(groups))
	for _, g := range groups {
		out = append(out, groupInfo(g))
	}
	writeJSON(w, out)
}

func (s *Server) syncGet(w http.ResponseWriter, r *http.Request) {
	if !s.syncAllowed(w, r) {
		return
	}
	id, err := ParseID(chi.URLParam(r, "groupId"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	g, err := s.SyncPlay.Get(r.Context(), principal(r), id)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	writeJSON(w, groupInfo(g))
}

func (s *Server) syncNew(w http.ResponseWriter, r *http.Request) {
	if !s.syncAllowed(w, r) {
		return
	}
	var req struct{ GroupName string }
	readBody(r, &req)
	name := strings.TrimSpace(req.GroupName)
	if len(name) > 80 {
		name = name[:80]
	}
	g, err := s.SyncPlay.Create(r.Context(), s.syncIdentity(r), name, nil, 0, 0)
	if err != nil {
		syncStatus(w, err)
		return
	}
	writeJSON(w, groupInfo(g))
}

func (s *Server) syncJoin(w http.ResponseWriter, r *http.Request) {
	if !s.syncAllowed(w, r) {
		return
	}
	var req struct{ GroupID string }
	readBody(r, &req)
	id, err := ParseID(req.GroupID)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	_, err = s.SyncPlay.Join(r.Context(), s.syncIdentity(r), id)
	syncStatus(w, err)
}

func (s *Server) syncLeave(w http.ResponseWriter, r *http.Request) {
	if !s.syncAllowed(w, r) {
		return
	}
	s.SyncPlay.Hub.Leave(sessionKey(principal(r)))
	w.WriteHeader(http.StatusNoContent)
}

// syncRequest is the union of the SyncPlay request bodies used here.
type syncRequest struct {
	PositionTicks       int64
	PlaylistItemID      string
	PlayingQueue        []string
	PlayingItemPosition int
	StartPositionTicks  int64
	ItemIDs             []string
	When                string
	IsPlaying           bool
}

func parseIDList(in []string) []uuid.UUID {
	out := make([]uuid.UUID, 0, len(in))
	for _, s := range in {
		if id, err := ParseID(s); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func (s *Server) syncCommand(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.syncAllowed(w, r) {
			return
		}
		var req syncRequest
		readBody(r, &req)
		m := syncplay.Message{Type: kind, PositionMS: req.PositionTicks / ticksPerMS}
		if id, err := ParseID(req.PlaylistItemID); err == nil {
			m.EntryID = id
		}
		switch kind {
		case "setQueue":
			m.ItemIDs, m.Index, m.StartMS = parseIDList(req.PlayingQueue), req.PlayingItemPosition, req.StartPositionTicks/ticksPerMS
		case "queue":
			m.ItemIDs = parseIDList(req.ItemIDs)
		case "stop":
			syncStatus(w, s.SyncPlay.Hub.Do(sessionKey(principal(r)), syncplay.Command{Kind: "stop"}))
			return
		}
		syncStatus(w, s.SyncPlay.Handle(r.Context(), s.syncIdentity(r), m))
	}
}

func (s *Server) syncReport(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.syncAllowed(w, r) {
			return
		}
		var req syncRequest
		readBody(r, &req)
		m := syncplay.Message{Type: kind, PositionMS: req.PositionTicks / ticksPerMS, Playing: req.IsPlaying}
		if id, err := ParseID(req.PlaylistItemID); err == nil {
			m.EntryID = id
		}
		if t, err := time.Parse(time.RFC3339Nano, req.When); err == nil {
			m.At = t.UnixMilli()
		}
		syncStatus(w, s.SyncPlay.Handle(r.Context(), s.syncIdentity(r), m))
	}
}

// syncTranslator turns group states into the socket messages media clients
// expect: group updates (joined, left, users, queue, state) and timed
// playback commands. It remembers the last state sent on its connection.
type syncTranslator struct {
	prev *syncplay.Snapshot
	now  func() time.Time
}

func groupUpdate(groupID uuid.UUID, kind string, data any) socketMessage {
	return socketMessage{MessageType: "SyncPlayGroupUpdate", Data: map[string]any{"GroupId": ID(groupID), "Type": kind, "Data": data}}
}

var queueReasons = map[string]string{"next": "NextItem", "previous": "PreviousItem", "jump": "SetCurrentItem", "queue": "Queue"}

var stateReasons = map[string]string{"play": "Unpause", "pause": "Pause", "seek": "Seek", "buffering": "Buffer", "ready": "Ready",
	"next": "NextItem", "previous": "PreviousItem", "jump": "SetPlaylistItem", "stop": "Stop", "ended": "Stop", "queue": "Queue",
	"setQueue": "Play", "create": "Play", "timeout": "IgnoreWait"}

func queueChanged(a, b syncplay.Snapshot) bool {
	if a.Index != b.Index || len(a.Queue) != len(b.Queue) {
		return true
	}
	for i := range a.Queue {
		if a.Queue[i].ID != b.Queue[i].ID {
			return true
		}
	}
	return false
}

func (t *syncTranslator) messages(e syncplay.Event) []socketMessage {
	if e.Correction != nil {
		return nil // media clients correct drift themselves
	}
	if e.Group == nil {
		if t.prev == nil {
			return nil
		}
		id := t.prev.ID
		t.prev = nil
		return []socketMessage{groupUpdate(id, "GroupLeft", ID(id))}
	}
	g := *e.Group
	var out []socketMessage
	prev := t.prev
	if prev == nil || prev.ID != g.ID {
		out = append(out, groupUpdate(g.ID, "GroupJoined", groupInfo(g)))
		prev = nil
	} else {
		old := map[uuid.UUID]string{}
		for _, m := range prev.Members {
			old[m.ID] = m.Name
		}
		for _, m := range g.Members {
			if _, ok := old[m.ID]; ok {
				delete(old, m.ID)
			} else {
				out = append(out, groupUpdate(g.ID, "UserJoined", m.Name))
			}
		}
		for _, name := range old {
			out = append(out, groupUpdate(g.ID, "UserLeft", name))
		}
	}
	if prev == nil || queueChanged(*prev, g) {
		reason := queueReasons[g.Reason]
		if reason == "" {
			reason = "NewPlaylist"
		}
		list := make([]map[string]string, 0, len(g.Queue))
		for _, q := range g.Queue {
			list = append(list, map[string]string{"ItemId": ID(q.ItemID), "PlaylistItemId": ID(q.ID)})
		}
		out = append(out, groupUpdate(g.ID, "PlayQueue", map[string]any{
			"Reason": reason, "LastUpdate": time.UnixMilli(g.ServerTime).UTC().Format(time.RFC3339Nano), "Playlist": list,
			"PlayingItemIndex": max(g.Index, 0), "StartPositionTicks": g.PositionMS * ticksPerMS,
			"IsPlaying": g.State == syncplay.Playing || g.State == syncplay.Waiting, "ShuffleMode": "Sorted", "RepeatMode": "RepeatNone"}))
	}
	if prev == nil || prev.State != g.State || prev.PositionMS != g.PositionMS || prev.AnchorAt != g.AnchorAt || prev.Index != g.Index {
		reason := stateReasons[g.Reason]
		if reason == "" {
			reason = "Ready"
		}
		out = append(out, groupUpdate(g.ID, "StateUpdate", map[string]any{"State": groupState(g.State), "Reason": reason}))
		if c := t.command(g); c != nil {
			out = append(out, *c)
		}
	}
	t.prev = &g
	return out
}

// command is the playback command for a group state: start at the anchor,
// pause, seek (a wait after a seek) or stop.
func (t *syncTranslator) command(g syncplay.Snapshot) *socketMessage {
	cur, ok := g.Current()
	if !ok && g.State != syncplay.Idle {
		return nil
	}
	now := t.now().UTC()
	var cmd string
	when := now
	switch g.State {
	case syncplay.Playing:
		cmd, when = "Unpause", time.UnixMilli(g.AnchorAt).UTC()
	case syncplay.Paused:
		cmd = "Pause"
	case syncplay.Waiting:
		cmd = "Pause"
		if g.Reason == "seek" {
			cmd = "Seek"
		}
	default:
		cmd = "Stop"
	}
	data := map[string]any{"GroupId": ID(g.ID), "When": when.Format(time.RFC3339Nano), "PositionTicks": g.PositionMS * ticksPerMS,
		"Command": cmd, "EmittedAt": now.Format(time.RFC3339Nano)}
	if ok {
		data["PlaylistItemId"] = ID(cur.ID)
	}
	return &socketMessage{MessageType: "SyncPlayCommand", Data: data}
}

// SyncIdentityFunc builds a member identity for a principal (name, device).
type SyncIdentityFunc func(ctx context.Context, p auth.Principal) syncplay.Identity
