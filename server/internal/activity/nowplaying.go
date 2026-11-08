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
