package auth

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Grants name who a signed media link was handed to (D-053): a session and the
// profile it used ("s_<session>_<profile>", "main" for the account itself), or
// an API key ("k_<key>"). A link bound to a grant stops working once the grant
// no longer holds: the session was signed out or revoked, it switched to
// another profile, the account was disabled or lost its library access, the
// key was deleted or its owner demoted.

// grantCacheTTL bounds how long a revoked grant keeps working.
const grantCacheTTL = 15 * time.Second

const maxGrantCache = 10000

// Grant returns the principal's grant ("" for a principal of neither kind).
func (p Principal) Grant() string {
	switch {
	case p.SessionID != uuid.Nil:
		profile := "main"
		if p.Profile != nil {
			profile = p.Profile.ID.String()
		}
		return "s_" + p.SessionID.String() + "_" + profile
	case p.APIKeyID != uuid.Nil:
		return "k_" + p.APIKeyID.String()
	}
	return ""
}

type grantEntry struct {
	ok bool
	at time.Time
}

type grantCache struct {
	mu sync.Mutex
	m  map[string]grantEntry
}

// CheckGrant reports whether grant still holds. Answers are cached for a few
// seconds: a player fetches many segments, each checked here.
func (s *Service) CheckGrant(ctx context.Context, grant string) bool {
	now := s.now()
	s.grants.mu.Lock()
	if e, ok := s.grants.m[grant]; ok && now.Sub(e.at) < grantCacheTTL {
		s.grants.mu.Unlock()
		return e.ok
	}
	s.grants.mu.Unlock()
	ok, err := s.grantHolds(ctx, grant)
	if err != nil {
		return false // not cached: a database hiccup must not lock players out for long
	}
	s.grants.mu.Lock()
	defer s.grants.mu.Unlock()
	if s.grants.m == nil || len(s.grants.m) >= maxGrantCache {
		s.grants.m = map[string]grantEntry{}
	}
	s.grants.m[grant] = grantEntry{ok: ok, at: now}
	return ok
}

// ForgetGrants drops cached answers (after sign-outs that must take effect at once).
func (s *Service) ForgetGrants() {
	s.grants.mu.Lock()
	s.grants.m = nil
	s.grants.mu.Unlock()
}

func (s *Service) grantHolds(ctx context.Context, grant string) (bool, error) {
	kind, rest, ok := strings.Cut(grant, "_")
	if !ok {
		return false, nil
	}
	var (
		isAdmin bool
		perms   []byte
		err     error
	)
	switch kind {
	case "s":
		sid, profile, ok := strings.Cut(rest, "_")
		if !ok {
			return false, nil
		}
		id, perr := uuid.Parse(sid)
		if perr != nil {
			return false, nil
		}
		var current *uuid.UUID
		err = s.pool.QueryRow(ctx, `SELECT s.profile_id, u.is_admin, u.permissions FROM sessions s JOIN users u ON u.id = s.user_id
			WHERE s.id = $1 AND s.expires_at > now() AND NOT u.disabled`, id).Scan(&current, &isAdmin, &perms)
		if err == nil {
			want := "main"
			if current != nil {
				want = current.String()
			}
			if want != profile {
				return false, nil
			}
		}
	case "k":
		id, perr := uuid.Parse(rest)
		if perr != nil {
			return false, nil
		}
		err = s.pool.QueryRow(ctx, `SELECT u.is_admin, u.permissions FROM api_keys k JOIN users u ON u.id = k.user_id
			WHERE k.id = $1 AND u.is_admin AND NOT u.disabled`, id).Scan(&isAdmin, &perms)
	default:
		return false, nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return isAdmin || parsePermissions(perms).AllLibraries, nil
}
