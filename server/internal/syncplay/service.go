package syncplay

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
)

// ItemInfo is what a group needs to know about a queued item.
type ItemInfo struct {
	Title      string
	DurationMS int64
}

// Lookup resolves an item for a viewer; an error means the viewer cannot open it.
type Lookup func(ctx context.Context, p auth.Principal, itemID uuid.UUID) (ItemInfo, error)

// Service checks permissions and library access in front of the hub. It is
// shared by Streamero's own API and the compat API.
type Service struct {
	Hub    *Hub
	Lookup Lookup
}

// Allowed reports whether a user may use watch-together.
func Allowed(p auth.Principal) bool { return p.User.IsAdmin || p.User.Permissions.SyncPlay }

// MaxQueue bounds a group's queue.
const MaxQueue = 200

// entries resolves items for the actor and checks every other viewer can open them too.
func (s *Service) entries(ctx context.Context, p auth.Principal, others []Identity, ids []uuid.UUID) ([]Entry, error) {
	if len(ids) > MaxQueue {
		return nil, ErrInvalid
	}
	out := make([]Entry, 0, len(ids))
	for _, id := range ids {
		info, err := s.Lookup(ctx, p, id)
		if err != nil {
			return nil, ErrForbidden
		}
		for _, o := range others {
			if _, err := s.Lookup(ctx, o.Principal, id); err != nil {
				return nil, ErrForbidden
			}
		}
		out = append(out, Entry{ID: uuid.New(), ItemID: id, Title: info.Title, DurationMS: info.DurationMS})
	}
	return out, nil
}

// canOpen reports whether p can open every queued item of a group.
func (s *Service) canOpen(ctx context.Context, p auth.Principal, g Snapshot) bool {
	seen := map[uuid.UUID]bool{}
	for _, e := range g.Queue {
		if seen[e.ItemID] {
			continue
		}
		seen[e.ItemID] = true
		if _, err := s.Lookup(ctx, p, e.ItemID); err != nil {
			return false
		}
	}
	return true
}

// Create starts a group for p (optionally with a queue).
func (s *Service) Create(ctx context.Context, id Identity, name string, itemIDs []uuid.UUID, index int, startMS int64) (Snapshot, error) {
	if !Allowed(id.Principal) {
		return Snapshot{}, ErrForbidden
	}
	entries, err := s.entries(ctx, id.Principal, nil, itemIDs)
	if err != nil {
		return Snapshot{}, err
	}
	return s.Hub.Create(id, name, entries, index, startMS)
}

// Join adds p to a group whose items p can open.
func (s *Service) Join(ctx context.Context, id Identity, groupID uuid.UUID) (Snapshot, error) {
	if !Allowed(id.Principal) {
		return Snapshot{}, ErrForbidden
	}
	g, ok := s.Hub.Group(groupID)
	if !ok {
		return Snapshot{}, ErrNotFound
	}
	if !s.canOpen(ctx, id.Principal, g) {
		return Snapshot{}, ErrForbidden
	}
	return s.Hub.Join(groupID, id)
}

// Get returns a group p may see.
func (s *Service) Get(ctx context.Context, p auth.Principal, groupID uuid.UUID) (Snapshot, error) {
	if !Allowed(p) {
		return Snapshot{}, ErrForbidden
	}
	g, ok := s.Hub.Group(groupID)
	if !ok || !s.canOpen(ctx, p, g) {
		return Snapshot{}, ErrNotFound
	}
	return g, nil
}

// List returns the groups p could join.
func (s *Service) List(ctx context.Context, p auth.Principal) ([]Snapshot, error) {
	if !Allowed(p) {
		return nil, ErrForbidden
	}
	out := []Snapshot{}
	for _, g := range s.Hub.List() {
		if s.canOpen(ctx, p, g) {
			out = append(out, g)
		}
	}
	return out, nil
}

// Message is a socket message from a client (the SyncClientMessage schema).
type Message struct {
	Type       string      `json:"type"`
	ClientTime int64       `json:"clientTime,omitempty"`
	PositionMS int64       `json:"positionMs,omitempty"`
	Speed      float64     `json:"speed,omitempty"`
	ItemIDs    []uuid.UUID `json:"itemIds,omitempty"`
	Index      int         `json:"index,omitempty"`
	StartMS    int64       `json:"startMs,omitempty"`
	EntryID    uuid.UUID   `json:"entryId,omitempty"`
	At         int64       `json:"at,omitempty"`
	Playing    bool        `json:"playing,omitempty"`
}

// Handle applies a client message (everything but ping) for a member.
func (s *Service) Handle(ctx context.Context, id Identity, m Message) error {
	if !Allowed(id.Principal) {
		return ErrForbidden
	}
	switch m.Type {
	case "ready", "buffering", "position":
		var at time.Time
		if m.At > 0 {
			at = time.UnixMilli(m.At)
		}
		return s.Hub.Report(id.Key, Report{Kind: m.Type, EntryID: m.EntryID, PositionMS: m.PositionMS, At: at, Playing: m.Playing})
	case "setQueue", "queue":
		var others []Identity
		for _, o := range s.Hub.Members(id.Key) {
			if o.Key != id.Key {
				others = append(others, o)
			}
		}
		entries, err := s.entries(ctx, id.Principal, others, m.ItemIDs)
		if err != nil {
			return err
		}
		return s.Hub.Do(id.Key, Command{Kind: m.Type, Entries: entries, Index: m.Index, PositionMS: m.StartMS})
	case "play", "pause", "seek", "speed", "next", "previous", "jump":
		return s.Hub.Do(id.Key, Command{Kind: m.Type, PositionMS: m.PositionMS, Speed: m.Speed, EntryID: m.EntryID})
	}
	return ErrInvalid
}

// Run ticks the hub until ctx ends.
func (s *Service) Run(ctx context.Context) {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			s.Hub.Tick()
		}
	}
}
