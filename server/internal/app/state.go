package app

import (
	"context"
	"sync/atomic"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// State tracks startup progress and exposes what handlers need.
type State struct {
	// SetupCode guards first-run setup; set before Bootstrap.
	SetupCode string

	// Library is set by the caller after Bootstrap (needs roots and a job inserter).
	library  atomic.Pointer[library.Service]
	stream   atomic.Pointer[stream.Server]
	ready    atomic.Bool
	auth     atomic.Pointer[auth.Service]
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

// Library returns the library service (nil until ready).
func (s *State) Library() *library.Service { return s.library.Load() }

// Stream returns the stream server (nil until ready).
func (s *State) Stream() *stream.Server { return s.stream.Load() }

// SetStream installs the stream server.
func (s *State) SetStream(v *stream.Server) { s.stream.Store(v) }

// SetLibrary installs the library service.
func (s *State) SetLibrary(l *library.Service) { s.library.Store(l) }

// Auth returns the account service (nil until ready).
func (s *State) Auth() *auth.Service { return s.auth.Load() }

// Settings returns the settings store (nil until ready).
func (s *State) Settings() *settings.Store { return s.settings.Load() }

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
