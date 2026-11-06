package app

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/settings"
)

// State tracks startup progress and exposes what handlers need.
type State struct {
	ready    atomic.Bool
	serverID atomic.Pointer[uuid.UUID]
	settings atomic.Pointer[settings.Store]
	lastErr  atomic.Pointer[string]
}

// Ready reports whether dependencies are connected and migrations are applied.
func (s *State) Ready() bool { return s.ready.Load() }

// ServerID returns the stable server ID (uuid.Nil until ready).
func (s *State) ServerID() uuid.UUID {
	if id := s.serverID.Load(); id != nil {
		return *id
	}
	return uuid.Nil
}

// SetupCompleted reports whether the first-run setup wizard has finished.
func (s *State) SetupCompleted(ctx context.Context) bool {
	st := s.settings.Load()
	if st == nil {
		return false
	}
	var done bool
	if _, err := st.Get(ctx, settings.KeySetupCompleted, &done); err != nil {
		return false
	}
	return done
}

// LastError returns the most recent startup error, if any.
func (s *State) LastError() string {
	if e := s.lastErr.Load(); e != nil {
		return *e
	}
	return ""
}

func (s *State) setErr(err error) {
	msg := err.Error()
	s.lastErr.Store(&msg)
}
