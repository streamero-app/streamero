// Package activity tracks what is being watched right now (for the admin dashboard).
package activity

import (
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Session is one client playing something.
type Session struct {
	Key        string // auth session or API key id
	User       string
	Profile    string
	Device     string
	Client     string
	ItemID     uuid.UUID
	Title      string
	Method     string
	Reason     string
	PositionMS int64
	DurationMS int64
	UpdatedAt  time.Time
}

// Registry is an in-memory view of active playback. Clients report progress
// every ~10 s while playing; a session silent for longer than TTL has stopped.
type Registry struct {
	TTL time.Duration
	Now func() time.Time

	mu       sync.Mutex
	sessions map[string]Session
	commands map[string][]Command
}

func (r *Registry) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func (r *Registry) ttl() time.Duration {
	if r.TTL > 0 {
		return r.TTL
	}
	return 90 * time.Second
}

// Start records a new playback (replacing whatever the session played before).
// It reports whether this is a different item than the session was playing.
func (r *Registry) Start(s Session) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.sessions == nil {
		r.sessions = map[string]Session{}
	}
	prev, had := r.sessions[s.Key]
	s.UpdatedAt = r.now()
	r.sessions[s.Key] = s
	return !had || prev.ItemID != s.ItemID || r.now().Sub(prev.UpdatedAt) > r.ttl()
}

// Progress updates the position of a known session; unknown sessions are ignored
// (a resume report after a restart re-appears with the next playback start).
func (r *Registry) Progress(key string, itemID uuid.UUID, positionMS int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[key]
	if !ok || s.ItemID != itemID {
		return
	}
	s.PositionMS, s.UpdatedAt = positionMS, r.now()
	r.sessions[key] = s
}

// Active returns live sessions, most recently updated first, and forgets stale ones.
func (r *Registry) Active() []Session {
	r.mu.Lock()
	defer r.mu.Unlock()
	cutoff := r.now().Add(-r.ttl())
	out := make([]Session, 0, len(r.sessions))
	for k, s := range r.sessions {
		if s.UpdatedAt.Before(cutoff) {
			delete(r.sessions, k)
			continue
		}
		out = append(out, s)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UpdatedAt.After(out[j].UpdatedAt) })
	return out
}

// Command is something an administrator asks a playing client to do.
type Command struct {
	Kind string // "message" or "stop"
	Text string
	At   time.Time
}

// Send queues a command for a live session; it reports whether the session exists.
func (r *Registry) Send(key string, c Command) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	s, ok := r.sessions[key]
	if !ok || s.UpdatedAt.Before(r.now().Add(-r.ttl())) {
		return false
	}
	if r.commands == nil {
		r.commands = map[string][]Command{}
	}
	c.At = r.now()
	// A client that stopped polling should not collect an endless queue.
	if q := r.commands[key]; len(q) < 20 {
		r.commands[key] = append(q, c)
	}
	if c.Kind == "stop" {
		delete(r.sessions, key)
	}
	return true
}

// Take returns and clears the queued commands of a session.
func (r *Registry) Take(key string) []Command {
	r.mu.Lock()
	defer r.mu.Unlock()
	q := r.commands[key]
	delete(r.commands, key)
	return q
}
