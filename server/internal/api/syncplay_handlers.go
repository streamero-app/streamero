package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/stream"
	"github.com/streamero-app/streamero/server/internal/syncplay"
)

// SyncLookup resolves an item for watch-together: the viewer must be able to
// open it (library access), like GET /items/{id}.
func (h *Handler) SyncLookup(ctx context.Context, p auth.Principal, itemID uuid.UUID) (syncplay.ItemInfo, error) {
	if h.lib() == nil {
		return syncplay.ItemInfo{}, errStarting
	}
	if !canSeeLibraries(p) {
		return syncplay.ItemInfo{}, syncplay.ErrForbidden
	}
	it, _, _, err := h.lib().GetItem(ctx, itemID, stream.Owner(p))
	if err != nil {
		return syncplay.ItemInfo{}, err
	}
	switch it.Kind {
	case "movie", "episode":
	default:
		return syncplay.ItemInfo{}, syncplay.ErrInvalid // folders, series and seasons are not playable
	}
	title := it.Title
	if it.Kind == "episode" && it.Season != nil && it.Episode != nil {
		var series string
		if it.ParentID != nil {
			_ = h.lib().Pool.QueryRow(ctx, `SELECT COALESCE(s.metadata_title, s.title) FROM items se
				JOIN items s ON s.id = se.parent_id WHERE se.id = $1`, *it.ParentID).Scan(&series)
		}
		title = strings.TrimSpace(series + " " + episodeLabel(*it.Season, *it.Episode) + " · " + it.Title)
	}
	info := syncplay.ItemInfo{Title: title}
	if it.DurationMS != nil {
		info.DurationMS = *it.DurationMS
	}
	return info, nil
}

func episodeLabel(season, episode int) string {
	return "S" + strconv.Itoa(season) + ":E" + strconv.Itoa(episode)
}

// SyncIdentity describes the caller as a group member: the profile or account
// name and the device of the session.
func (h *Handler) SyncIdentity(ctx context.Context, p auth.Principal) syncplay.Identity {
	id := syncplay.Identity{Key: sessionKey(p), UserID: p.User.ID, Name: p.User.Username, Principal: p}
	if h.lib() != nil {
		var display string
		_ = h.lib().Pool.QueryRow(ctx, `SELECT COALESCE(display_name, '') FROM users WHERE id = $1`, p.User.ID).Scan(&display)
		if display != "" {
			id.Name = display
		}
		if p.SessionID != uuid.Nil {
			_ = h.lib().Pool.QueryRow(ctx, `SELECT device_name FROM sessions WHERE id = $1`, p.SessionID).Scan(&id.Device)
		}
	}
	if p.Profile != nil {
		id.Name = p.Profile.Name
	}
	return id
}

func (h *Handler) syncService() (*syncplay.Service, error) {
	if h.SyncPlay == nil || h.lib() == nil {
		return nil, errStarting
	}
	return h.SyncPlay, nil
}

func mapSyncError(err error) *StatusError {
	switch {
	case errors.Is(err, syncplay.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, syncplay.ErrForbidden):
		return &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "watch together is not allowed for this account or item"}
	case errors.Is(err, syncplay.ErrRateLimited):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: err.Error()}
	case errors.Is(err, syncplay.ErrTooManyGroups):
		return &StatusError{Status: http.StatusServiceUnavailable, Code: "too_many_groups", Message: err.Error()}
	case errors.Is(err, syncplay.ErrInvalid):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	case errors.Is(err, syncplay.ErrNotInGroup):
		return &StatusError{Status: http.StatusConflict, Code: "not_in_group", Message: err.Error()}
	}
	return nil
}

func toSyncGroup(s syncplay.Snapshot) gen.SyncGroup {
	out := gen.SyncGroup{ID: s.ID, Name: s.Name, State: gen.SyncGroupState(s.State), Queue: make([]gen.SyncQueueEntry, 0, len(s.Queue)),
		Index: s.Index, PositionMs: s.PositionMS, AnchorAt: s.AnchorAt, Speed: s.Speed, Version: s.Version,
		Members: make([]gen.SyncMember, 0, len(s.Members)), ServerTime: s.ServerTime, CreatedAt: s.CreatedAt,
		Reason: optString(s.Reason), Actor: optString(s.Actor)}
	for _, e := range s.Queue {
		q := gen.SyncQueueEntry{ID: e.ID, ItemId: e.ItemID, Title: e.Title}
		if e.DurationMS > 0 {
			q.DurationMs = gen.NewOptInt64(e.DurationMS)
		}
		out.Queue = append(out.Queue, q)
	}
	for _, m := range s.Members {
		out.Members = append(out.Members, gen.SyncMember{ID: m.ID, Name: m.Name, Device: m.Device, Ready: m.Ready,
			Buffering: m.Buffering, Connected: m.Connected, Lagging: m.Lagging})
	}
	if s.SelfID != nil {
		out.SelfId = gen.NewOptUUID(*s.SelfID)
	}
	return out
}

// ListSyncGroups implements GET /syncplay/groups.
func (h *Handler) ListSyncGroups(ctx context.Context) ([]gen.SyncGroup, error) {
	svc, err := h.syncService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	groups, err := svc.List(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]gen.SyncGroup, 0, len(groups))
	for _, g := range groups {
		out = append(out, toSyncGroup(g))
	}
	return out, nil
}

// CreateSyncGroup implements POST /syncplay/groups.
func (h *Handler) CreateSyncGroup(ctx context.Context, req *gen.CreateSyncGroupRequest) (*gen.SyncGroup, error) {
	svc, err := h.syncService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	g, err := svc.Create(ctx, h.SyncIdentity(ctx, p), strings.TrimSpace(req.Name.Value), req.ItemIds, req.Index.Or(0), req.StartMs.Or(0))
	if err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "syncplay.create", map[string]any{"group": g.ID})
	out := toSyncGroup(g)
	return &out, nil
}

// GetSyncGroup implements GET /syncplay/groups/{groupId}.
func (h *Handler) GetSyncGroup(ctx context.Context, params gen.GetSyncGroupParams) (*gen.SyncGroup, error) {
	svc, err := h.syncService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	g, err := svc.Get(ctx, p, params.GroupId)
	if err != nil {
		return nil, err
	}
	out := toSyncGroup(g)
	return &out, nil
}

// JoinSyncGroup implements POST /syncplay/groups/{groupId}/join.
func (h *Handler) JoinSyncGroup(ctx context.Context, params gen.JoinSyncGroupParams) (*gen.SyncGroup, error) {
	svc, err := h.syncService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	g, err := svc.Join(ctx, h.SyncIdentity(ctx, p), params.GroupId)
	if err != nil {
		return nil, err
	}
	out := toSyncGroup(g)
	return &out, nil
}

// GetMySyncGroup implements GET /syncplay/me.
func (h *Handler) GetMySyncGroup(ctx context.Context) (gen.GetMySyncGroupRes, error) {
	svc, err := h.syncService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	g, ok := svc.Hub.GroupOf(sessionKey(p))
	if !ok {
		return &gen.GetMySyncGroupNoContent{}, nil
	}
	out := toSyncGroup(g)
	return &out, nil
}

// LeaveSyncGroup implements POST /syncplay/leave.
func (h *Handler) LeaveSyncGroup(ctx context.Context) error {
	svc, err := h.syncService()
	if err != nil {
		return err
	}
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	svc.Hub.Leave(sessionKey(p))
	return nil
}

// SyncPlaySocket serves GET /api/v1/syncplay/socket (outside the generated
// router: it upgrades to a WebSocket). The web client's session cookie has
// already become a bearer header (same-origin only: the socket checks Origin).
func (h *Handler) SyncPlaySocket(w http.ResponseWriter, r *http.Request) {
	if !h.State.Ready() || h.Auth == nil || h.Auth() == nil || h.SyncPlay == nil {
		http.Error(w, "server is starting", http.StatusServiceUnavailable)
		return
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	p, err := h.Auth().Authenticate(r.Context(), token)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if !syncplay.Allowed(p) {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	h.SyncPlay.ServeSocket(w, r, h.SyncIdentity(r.Context(), p))
}
