// Package syncplay runs watch-together groups: a shared queue, play, pause,
// seek and speed for everyone in a group, a wait while someone buffers, and
// drift correction from the position reports of the players (D-036).
//
// Groups live in memory. A member is a session (sign-in), not a connection, so
// a player that reconnects stays in its group; members without a connection
// never hold the others up and are dropped after MemberTimeout.
package syncplay

import (
	"errors"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
)

// State of a group.
type State string

// Group states.
const (
	Idle    State = "idle"    // nothing to play (empty queue or the queue ended)
	Waiting State = "waiting" // should play, waits until every connected member is ready
	Paused  State = "paused"
	Playing State = "playing"
)

// Errors of group operations.
var (
	ErrNotFound      = errors.New("group not found")
	ErrNotInGroup    = errors.New("not in a group")
	ErrRateLimited   = errors.New("too many group actions")
	ErrInvalid       = errors.New("invalid group action")
	ErrTooManyGroups = errors.New("too many groups")
	ErrForbidden     = errors.New("not allowed")
)

// Identity is a member as the hub knows it.
type Identity struct {
	// Key identifies the session (sign-in); it survives reconnects.
	Key    string
	UserID uuid.UUID
	Name   string
	Device string
	// Compat members are media clients of the compat API; they correct drift
	// themselves and get no server corrections.
	Compat bool
	// Principal is used for library access checks of queued items.
	Principal auth.Principal
}

// Entry is one queued item.
type Entry struct {
	ID         uuid.UUID `json:"id"`
	ItemID     uuid.UUID `json:"itemId"`
	Title      string    `json:"title"`
	DurationMS int64     `json:"durationMs,omitempty"`
}

// MemberView is a member as other members see it.
type MemberView struct {
	ID        uuid.UUID `json:"id"`
	Name      string    `json:"name"`
	Device    string    `json:"device"`
	Ready     bool      `json:"ready"`
	Buffering bool      `json:"buffering"`
	Connected bool      `json:"connected"`
	Lagging   bool      `json:"lagging"`
	UserID    uuid.UUID `json:"-"`
	Compat    bool      `json:"-"`
}

// Snapshot is the whole state of a group (the SyncGroup schema).
type Snapshot struct {
	ID         uuid.UUID    `json:"id"`
	Name       string       `json:"name"`
	State      State        `json:"state"`
	Queue      []Entry      `json:"queue"`
	Index      int          `json:"index"`
	PositionMS int64        `json:"positionMs"`
	AnchorAt   int64        `json:"anchorAt"`
	Speed      float64      `json:"speed"`
	Version    int64        `json:"version"`
	Reason     string       `json:"reason,omitempty"`
	Actor      string       `json:"actor,omitempty"`
	Members    []MemberView `json:"members"`
	ServerTime int64        `json:"serverTime"`
	CreatedAt  time.Time    `json:"createdAt"`
	SelfID     *uuid.UUID   `json:"selfId,omitempty"`
}

// Current returns the playing entry, if any.
func (s Snapshot) Current() (Entry, bool) {
	if s.Index < 0 || s.Index >= len(s.Queue) {
		return Entry{}, false
	}
	return s.Queue[s.Index], true
}

// PositionAt is the shared position at server time t.
func (s Snapshot) PositionAt(t time.Time) int64 {
	return positionAt(s.State, s.PositionMS, time.UnixMilli(s.AnchorAt), s.Speed, t)
}

// Event is delivered to a member's connections.
type Event struct {
	// Group is the new state; nil means the member is no longer in a group.
	Group *Snapshot
	// Reason explains a nil Group: left, removed or closed.
	Reason string
	// Correction is drift correction for this member.
	Correction *Correction
	At         time.Time
}

type member struct {
	Identity
	id        uuid.UUID
	ready     bool
	buffering bool
	lagging   bool
	lastSeen  time.Time
	corrUntil time.Time
}

type group struct {
	id        uuid.UUID
	name      string
	created   time.Time
	state     State
	wantPlay  bool
	queue     []Entry
	index     int
	pos       int64
	anchor    time.Time
	speed     float64
	version   int64
	reason    string
	actor     string
	waitSince time.Time
	members   []*member
}

func (g *group) current() (Entry, bool) {
	if g.index < 0 || g.index >= len(g.queue) {
		return Entry{}, false
	}
	return g.queue[g.index], true
}

func (g *group) member(key string) *member {
	for _, m := range g.members {
		if m.Key == key {
			return m
		}
	}
	return nil
}

func positionAt(st State, pos int64, anchor time.Time, speed float64, t time.Time) int64 {
	if st != Playing || !t.After(anchor) {
		return pos
	}
	return pos + int64(float64(t.Sub(anchor).Milliseconds())*speed)
}

func (g *group) positionAt(t time.Time) int64 {
	return positionAt(g.state, g.pos, g.anchor, g.speed, t)
}

type sub struct{ ch chan Event }

// Hub holds all groups. The zero value is not usable; use NewHub.
type Hub struct {
	Now func() time.Time
	// StartDelay is how far in the future a synchronised start is scheduled,
	// so every member receives it before it happens.
	StartDelay time.Duration
	// WaitTimeout bounds a wait for buffering members; then the group goes on
	// without them (they catch up through drift correction).
	WaitTimeout time.Duration
	// MemberTimeout drops members that have had no connection for this long.
	MemberTimeout time.Duration
	MaxGroups     int

	mu     sync.Mutex
	groups map[uuid.UUID]*group
	byKey  map[string]*group
	subs   map[string]map[*sub]struct{}
	limits map[string]*limits
}

// NewHub returns a hub with default timings.
func NewHub() *Hub {
	return &Hub{StartDelay: 600 * time.Millisecond, WaitTimeout: 20 * time.Second, MemberTimeout: 2 * time.Minute, MaxGroups: 200,
		groups: map[uuid.UUID]*group{}, byKey: map[string]*group{}, subs: map[string]map[*sub]struct{}{}, limits: map[string]*limits{}}
}

func (h *Hub) now() time.Time {
	if h.Now != nil {
		return h.Now()
	}
	return time.Now()
}

func (h *Hub) connected(key string) bool { return len(h.subs[key]) > 0 }

func (h *Hub) snapshot(g *group, now time.Time) Snapshot {
	s := Snapshot{ID: g.id, Name: g.name, State: g.state, Queue: append([]Entry{}, g.queue...), Index: g.index,
		PositionMS: g.pos, AnchorAt: g.anchor.UnixMilli(), Speed: g.speed, Version: g.version, Reason: g.reason, Actor: g.actor,
		ServerTime: now.UnixMilli(), CreatedAt: g.created, Members: make([]MemberView, 0, len(g.members))}
	for _, m := range g.members {
		s.Members = append(s.Members, MemberView{ID: m.id, Name: m.Name, Device: m.Device, Ready: m.ready, Buffering: m.buffering,
			Connected: h.connected(m.Key), Lagging: m.lagging, UserID: m.UserID, Compat: m.Compat})
	}
	return s
}

func (h *Hub) selfSnapshot(g *group, key string, now time.Time) Snapshot {
	s := h.snapshot(g, now)
	if m := g.member(key); m != nil {
		id := m.id
		s.SelfID = &id
	}
	return s
}

func (h *Hub) send(key string, e Event) {
	for s := range h.subs[key] {
		select {
		case s.ch <- e:
		default: // a stuck connection; the next state supersedes this one
		}
	}
}

// changed records a change and sends the new state to every member.
func (h *Hub) changed(g *group, reason, actor string, now time.Time) {
	g.version++
	g.reason, g.actor = reason, actor
	snap := h.snapshot(g, now)
	for _, m := range g.members {
		if !h.connected(m.Key) {
			continue
		}
		s := snap
		s.Members = append([]MemberView{}, snap.Members...)
		id := m.id
		s.SelfID = &id
		h.send(m.Key, Event{Group: &s, At: now})
	}
}

// blocking reports whether a member holds up a synchronised start.
func (h *Hub) blocking(m *member) bool { return h.connected(m.Key) && !m.ready && !m.lagging }

// settle starts a waiting group once nobody holds it up and somebody is ready.
func (h *Hub) settle(g *group, now time.Time) {
	if g.state != Waiting || !g.wantPlay {
		return
	}
	if _, ok := g.current(); !ok {
		g.state, g.wantPlay = Idle, false
		return
	}
	anyReady := false
	for _, m := range g.members {
		if h.blocking(m) {
			return
		}
		anyReady = anyReady || (m.ready && h.connected(m.Key))
	}
	// Somebody has to have the item loaded: a group whose members are all
	// offline (or just created) keeps waiting instead of running unseen.
	if !anyReady {
		return
	}
	g.state = Playing
	g.anchor = now.Add(h.StartDelay)
}

func (h *Hub) wait(g *group, now time.Time) {
	if g.state != Waiting {
		g.waitSince = now
	}
	g.state, g.wantPlay = Waiting, true
}

func (h *Hub) unready(g *group) {
	for _, m := range g.members {
		m.ready, m.buffering, m.lagging = false, false, false
	}
}

func (h *Hub) newGroup(name string, now time.Time) *group {
	return &group{id: uuid.New(), name: name, created: now, state: Idle, index: -1, speed: 1, anchor: now}
}

// leave removes a member from its group; an empty group is closed.
func (h *Hub) leave(key, reason string, now time.Time) {
	g := h.byKey[key]
	if g == nil {
		return
	}
	delete(h.byKey, key)
	name := ""
	for i, m := range g.members {
		if m.Key == key {
			name = m.Name
			g.members = append(g.members[:i], g.members[i+1:]...)
			break
		}
	}
	h.send(key, Event{Reason: reason, At: now})
	if len(g.members) == 0 {
		delete(h.groups, g.id)
		return
	}
	h.settle(g, now)
	h.changed(g, "leave", name, now)
}

func (h *Hub) add(g *group, id Identity, now time.Time) {
	h.leave(id.Key, "left", now)
	g.members = append(g.members, &member{Identity: id, id: uuid.New(), lastSeen: now})
	h.byKey[id.Key] = g
}

// Create starts a group with the creator as its first member (who leaves any
// other group). With entries, the group waits for its members and then plays.
func (h *Hub) Create(id Identity, name string, entries []Entry, index int, startMS int64) (Snapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if !h.limit(id.Key).cmd.allow(now, cmdRate, cmdBurst) {
		return Snapshot{}, ErrRateLimited
	}
	if len(h.groups) >= h.MaxGroups {
		return Snapshot{}, ErrTooManyGroups
	}
	if name == "" {
		name = id.Name
	}
	g := h.newGroup(name, now)
	h.groups[g.id] = g
	h.add(g, id, now)
	if len(entries) > 0 {
		if index < 0 || index >= len(entries) {
			index = 0
		}
		g.queue, g.index, g.pos = entries, index, max(startMS, 0)
		h.wait(g, now)
		h.settle(g, now)
	}
	h.changed(g, "create", id.Name, now)
	return h.selfSnapshot(g, id.Key, now), nil
}

// Join adds a member to a group (leaving any other). The group does not stop:
// the new member loads the current item and catches up.
func (h *Hub) Join(groupID uuid.UUID, id Identity) (Snapshot, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	if !h.limit(id.Key).cmd.allow(now, cmdRate, cmdBurst) {
		return Snapshot{}, ErrRateLimited
	}
	g := h.groups[groupID]
	if g == nil {
		return Snapshot{}, ErrNotFound
	}
	if m := g.member(id.Key); m != nil {
		return h.selfSnapshot(g, id.Key, now), nil
	}
	h.add(g, id, now)
	h.changed(g, "join", id.Name, now)
	return h.selfSnapshot(g, id.Key, now), nil
}

// Leave takes the member out of its group; the others play on.
func (h *Hub) Leave(key string) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.byKey[key] == nil {
		return false
	}
	h.leave(key, "left", h.now())
	return true
}

// Group returns a group's state.
func (h *Hub) Group(groupID uuid.UUID) (Snapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.groups[groupID]
	if g == nil {
		return Snapshot{}, false
	}
	return h.snapshot(g, h.now()), true
}

// GroupOf returns the group of a member, with SelfID set.
func (h *Hub) GroupOf(key string) (Snapshot, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.byKey[key]
	if g == nil {
		return Snapshot{}, false
	}
	return h.selfSnapshot(g, key, h.now()), true
}

// List returns all groups, newest first.
func (h *Hub) List() []Snapshot {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	out := make([]Snapshot, 0, len(h.groups))
	for _, g := range h.groups {
		out = append(out, h.snapshot(g, now))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	return out
}

// Members returns the identities in a member's group (for access checks).
func (h *Hub) Members(key string) []Identity {
	h.mu.Lock()
	defer h.mu.Unlock()
	g := h.byKey[key]
	if g == nil {
		return nil
	}
	out := make([]Identity, 0, len(g.members))
	for _, m := range g.members {
		out = append(out, m.Identity)
	}
	return out
}

// Command is a group action of a member.
type Command struct {
	Kind       string // play, pause, seek, speed, setQueue, queue, next, previous, jump, stop
	PositionMS int64
	Speed      float64
	Entries    []Entry // setQueue, queue (resolved and access-checked by the caller)
	Index      int
	EntryID    uuid.UUID // next, previous: the entry the sender is on (zero = any); jump: target
}

// Speed limits.
const (
	MinSpeed = 0.25
	MaxSpeed = 3.0
)

// Do applies a member's command and sends the new state to the group.
func (h *Hub) Do(key string, c Command) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	g := h.byKey[key]
	if g == nil {
		return ErrNotInGroup
	}
	m := g.member(key)
	m.lastSeen = now
	if !h.limit(key).cmd.allow(now, cmdRate, cmdBurst) {
		return ErrRateLimited
	}
	cur, hasCur := g.current()
	reason := c.Kind
	switch c.Kind {
	case "play":
		if !hasCur {
			return ErrInvalid
		}
		if g.state == Playing || g.state == Waiting {
			return nil
		}
		h.wait(g, now)
	case "pause":
		switch g.state {
		case Playing:
			g.pos = g.positionAt(now)
		case Waiting:
		default:
			return nil
		}
		g.state, g.wantPlay, g.anchor = Paused, false, now
	case "seek":
		if !hasCur || c.PositionMS < 0 {
			return ErrInvalid
		}
		pos := c.PositionMS
		if cur.DurationMS > 0 && pos > cur.DurationMS {
			pos = cur.DurationMS
		}
		play := g.state == Playing || g.wantPlay
		g.pos, g.anchor = pos, now
		h.unready(g)
		if play {
			h.wait(g, now)
		} else {
			g.state = Paused
		}
	case "speed":
		if c.Speed < MinSpeed || c.Speed > MaxSpeed {
			return ErrInvalid
		}
		if g.state == Playing {
			g.pos, g.anchor = g.positionAt(now), now
		}
		g.speed = c.Speed
	case "setQueue":
		if len(c.Entries) == 0 {
			return ErrInvalid
		}
		idx := c.Index
		if idx < 0 || idx >= len(c.Entries) {
			idx = 0
		}
		g.queue, g.index, g.pos, g.anchor = c.Entries, idx, max(c.PositionMS, 0), now
		h.unready(g)
		h.wait(g, now)
	case "queue":
		if len(c.Entries) == 0 {
			return ErrInvalid
		}
		g.queue = append(g.queue, c.Entries...)
		if !hasCur {
			g.index, g.pos, g.anchor = len(g.queue)-len(c.Entries), 0, now
			h.unready(g)
			h.wait(g, now)
		}
	case "next", "previous", "jump":
		if !hasCur {
			return nil
		}
		target := g.index
		switch c.Kind {
		case "next":
			if c.EntryID != uuid.Nil && c.EntryID != cur.ID {
				return nil // someone else already moved on
			}
			target++
		case "previous":
			if c.EntryID != uuid.Nil && c.EntryID != cur.ID {
				return nil
			}
			target = max(target-1, 0)
		case "jump":
			target = -1
			for i, e := range g.queue {
				if e.ID == c.EntryID {
					target = i
				}
			}
			if target < 0 {
				return ErrInvalid
			}
		}
		if target >= len(g.queue) {
			g.state, g.wantPlay, g.pos, g.anchor = Idle, false, 0, now
			reason = "ended"
			break
		}
		g.index, g.pos, g.anchor = target, 0, now
		h.unready(g)
		h.wait(g, now)
	case "stop":
		g.state, g.wantPlay, g.pos, g.anchor = Idle, false, 0, now
	default:
		return ErrInvalid
	}
	h.settle(g, now)
	h.changed(g, reason, m.Name, now)
	return nil
}

// Report is what a member's player says about itself.
type Report struct {
	Kind       string // ready, buffering, position
	EntryID    uuid.UUID
	PositionMS int64
	// At is when PositionMS was read, on the server's clock.
	At      time.Time
	Playing bool
}

// Report handles a player report: readiness, buffering and drift correction.
// Reports about another entry than the current one are stale and ignored.
func (h *Hub) Report(key string, r Report) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	g := h.byKey[key]
	if g == nil {
		return ErrNotInGroup
	}
	m := g.member(key)
	m.lastSeen = now
	if !h.limit(key).report.allow(now, reportRate, reportBurst) {
		return ErrRateLimited
	}
	cur, ok := g.current()
	if !ok || (r.EntryID != uuid.Nil && r.EntryID != cur.ID) {
		return nil
	}
	if r.At.IsZero() || r.At.Sub(now).Abs() > 10*time.Second {
		r.At = now // a client without a usable clock offset
	}
	switch r.Kind {
	case "ready":
		if m.ready && !m.buffering && !m.lagging {
			return nil
		}
		m.ready, m.buffering, m.lagging = true, false, false
		m.corrUntil = now.Add(time.Second) // let the player settle before judging drift
		h.settle(g, now)
		h.changed(g, "ready", m.Name, now)
	case "buffering":
		if m.buffering {
			return nil
		}
		m.ready, m.buffering = false, true
		if g.state == Playing {
			g.pos, g.anchor = g.positionAt(now), now
			h.wait(g, now)
		}
		h.changed(g, "buffering", m.Name, now)
	case "position":
		if g.state != Playing || !m.ready || m.Compat || now.Before(m.corrUntil) || r.At.Before(g.anchor) {
			return nil
		}
		c := Drift(r.PositionMS-g.positionAt(r.At), g.speed)
		if c == nil {
			return nil
		}
		if c.Mode == "seek" {
			c.PositionMS = g.positionAt(now)
			m.corrUntil = now.Add(3 * time.Second)
		} else {
			m.corrUntil = now.Add(time.Duration(c.DurationMS)*time.Millisecond + 500*time.Millisecond)
		}
		h.send(key, Event{Correction: c, At: now})
	default:
		return ErrInvalid
	}
	return nil
}

// Subscribe registers a connection of a session; events for that member are
// delivered on the channel until the returned function is called. A
// (re)connecting member gets the current state right away.
func (h *Hub) Subscribe(key string) (<-chan Event, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	s := &sub{ch: make(chan Event, 64)}
	first := !h.connected(key)
	if h.subs[key] == nil {
		h.subs[key] = map[*sub]struct{}{}
	}
	h.subs[key][s] = struct{}{}
	if g := h.byKey[key]; g != nil {
		m := g.member(key)
		m.lastSeen = now
		if first {
			// The player may have restarted: it confirms readiness again.
			m.ready, m.buffering = false, false
			h.changed(g, "connect", m.Name, now)
		} else {
			snap := h.selfSnapshot(g, key, now)
			s.ch <- Event{Group: &snap, At: now}
		}
	}
	return s.ch, func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		delete(h.subs[key], s)
		if len(h.subs[key]) > 0 {
			return
		}
		delete(h.subs, key)
		if g := h.byKey[key]; g != nil {
			m := g.member(key)
			m.lastSeen, m.buffering = h.now(), false
			h.settle(g, h.now())
			h.changed(g, "disconnect", m.Name, h.now())
		}
	}
}

// Tick expires disconnected members and ends waits that took too long. The
// server calls it about once a second.
func (h *Hub) Tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	now := h.now()
	for _, g := range h.groups {
		var gone []string
		for _, m := range g.members {
			if !h.connected(m.Key) && now.Sub(m.lastSeen) > h.MemberTimeout {
				gone = append(gone, m.Key)
			}
		}
		for _, k := range gone {
			h.leave(k, "removed", now)
		}
		if h.groups[g.id] == nil {
			continue
		}
		if g.state == Waiting && g.wantPlay && now.Sub(g.waitSince) > h.WaitTimeout {
			for _, m := range g.members {
				if h.blocking(m) {
					m.lagging = true
				}
			}
			h.settle(g, now)
			h.changed(g, "timeout", "", now)
		}
	}
	for k, l := range h.limits {
		if h.byKey[k] == nil && !h.connected(k) && now.Sub(l.cmd.last) > 10*time.Minute && now.Sub(l.report.last) > 10*time.Minute {
			delete(h.limits, k)
		}
	}
}

// Rate limits per session: group actions and player reports.
const (
	cmdRate     = 3.0 // per second
	cmdBurst    = 12
	reportRate  = 10.0
	reportBurst = 30
)

type bucket struct {
	tokens float64
	last   time.Time
}

func (b *bucket) allow(now time.Time, rate float64, burst int) bool {
	if b.last.IsZero() {
		b.tokens = float64(burst)
	} else {
		b.tokens = min(float64(burst), b.tokens+now.Sub(b.last).Seconds()*rate)
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

type limits struct{ cmd, report bucket }

func (h *Hub) limit(key string) *limits {
	l := h.limits[key]
	if l == nil {
		l = &limits{}
		h.limits[key] = l
	}
	return l
}
