package adult

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/secretbox"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// Errors for the API layer.
var (
	// ErrNotFound is what every adult endpoint answers to a caller without access:
	// the same 404 as an unknown address, so the section cannot be discovered.
	ErrNotFound = library.ErrNotFound
	// ErrLocked: the viewer set a PIN and has not entered it in this session.
	ErrLocked = errors.New("enter your adult section PIN first")
	// ErrInvalidPIN: wrong PIN (counts towards the sign-in ban).
	ErrInvalidPIN = errors.New("wrong PIN")
	// ErrBadPIN: a new PIN that is not 4–12 digits.
	ErrBadPIN = errors.New("the PIN must be 4 to 12 digits")
	// ErrConfirm: an adult library or adult access needs an explicit confirmation.
	ErrConfirm = errors.New("confirm that this is adult content only adults may see")
	// ErrBadKey: ThePornDB or StashDB refused the key.
	ErrBadKey = errors.New("the metadata service refused the API key")
	// ErrProvider: the metadata service could not be reached or answered badly.
	ErrProvider = errors.New("the metadata service is unavailable")
)

// Kind is the library kind of adult libraries.
const Kind = "adult"

// UnlockTTL is how long an entered PIN keeps a session unlocked.
const UnlockTTL = 4 * time.Hour

// Service is the adult library: access, browsing, scanning and metadata.
type Service struct {
	Pool  *pgxpool.Pool
	Roots library.Roots
	// ImageDir holds downloaded artwork (<dir>/<owner>/<uuid>/<type>.jpg); it is
	// not under the items' image folder, so no other endpoint can serve it.
	ImageDir string
	Settings *settings.Store
	// Box encrypts the ThePornDB / StashDB keys (nil: keys cannot be saved).
	Box   *secretbox.Box
	Probe library.Prober
	// FFmpeg grabs a frame as artwork for scenes without metadata ("" = none).
	FFmpeg string
	Log    *slog.Logger
	// HTTP talks to ThePornDB and StashDB (nil = a client with a 15 s timeout).
	HTTP *http.Client
	// TPDBURL and StashDBURL override the service addresses (tests).
	TPDBURL, StashDBURL string

	mu       sync.Mutex
	unlocked map[uuid.UUID]time.Time // session → until
	now      func() time.Time
}

func (s *Service) clock() time.Time {
	if s.now != nil {
		return s.now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

// Viewer is a caller allowed into the adult section.
type Viewer struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
	IsAdmin   bool
}

// HasAccess reports whether the account was given adult access (regardless of
// the session; the admin user list shows it).
func (s *Service) HasAccess(ctx context.Context, userID uuid.UUID) (bool, error) {
	var ok bool
	err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM adult_access WHERE user_id = $1)`, userID).Scan(&ok)
	return ok, err
}

// GrantedUsers returns the accounts with adult access.
func (s *Service) GrantedUsers(ctx context.Context) (map[uuid.UUID]bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT user_id FROM adult_access`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[uuid.UUID]bool{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		out[id] = true
	}
	return out, rows.Err()
}

// Access is the gate of every adult endpoint: a signed-in session (never an API
// key) of an account given adult access, outside a kids profile, while an adult
// library exists. Everything else is ErrNotFound.
func (s *Service) Access(ctx context.Context, p auth.Principal) (Viewer, error) {
	if p.User.ID == uuid.Nil || p.SessionID == uuid.Nil || p.APIKeyID != uuid.Nil || p.IsKid() || p.User.Disabled {
		return Viewer{}, ErrNotFound
	}
	var ok bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM adult_access WHERE user_id = $1)
		AND EXISTS (SELECT 1 FROM libraries WHERE kind = 'adult')`, p.User.ID).Scan(&ok); err != nil {
		return Viewer{}, err
	}
	if !ok {
		return Viewer{}, ErrNotFound
	}
	return Viewer{UserID: p.User.ID, SessionID: p.SessionID, IsAdmin: p.User.IsAdmin}, nil
}

// Open is Access plus the session PIN: content endpoints answer ErrLocked until
// a viewer who set a PIN enters it.
func (s *Service) Open(ctx context.Context, p auth.Principal) (Viewer, error) {
	v, err := s.Access(ctx, p)
	if err != nil {
		return v, err
	}
	pinSet, _, err := s.prefs(ctx, v.UserID)
	if err != nil {
		return v, err
	}
	if pinSet != "" && !s.isUnlocked(v.SessionID) {
		return v, ErrLocked
	}
	return v, nil
}

// Available reports whether the adult section is open to this principal (for
// /auth/me): Access without the PIN.
func (s *Service) Available(ctx context.Context, p auth.Principal) bool {
	_, err := s.Access(ctx, p)
	return err == nil
}

func (s *Service) isUnlocked(session uuid.UUID) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	until, ok := s.unlocked[session]
	if ok && s.clock().After(until) {
		delete(s.unlocked, session)
		return false
	}
	return ok
}

func (s *Service) setUnlocked(session uuid.UUID, on bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.unlocked == nil {
		s.unlocked = map[uuid.UUID]time.Time{}
	}
	now := s.clock()
	for k, v := range s.unlocked {
		if now.After(v) {
			delete(s.unlocked, k)
		}
	}
	if on {
		s.unlocked[session] = now.Add(UnlockTTL)
	} else {
		delete(s.unlocked, session)
	}
}

// prefs returns the PIN hash ("" = none) and blurring of an account.
func (s *Service) prefs(ctx context.Context, userID uuid.UUID) (pinHash string, blur bool, err error) {
	var h *string
	err = s.Pool.QueryRow(ctx, `SELECT pin_hash, blur FROM adult_prefs WHERE user_id = $1`, userID).Scan(&h, &blur)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", true, nil
	}
	if h != nil {
		pinHash = *h
	}
	return pinHash, blur, err
}

// Status is what the section shows about itself.
type Status struct {
	PinSet, Unlocked, Blur, MetadataConfigured bool
	Libraries                                  []LibraryInfo
}

// LibraryInfo is an adult library with its scene count.
type LibraryInfo struct {
	ID         uuid.UUID
	Name       string
	SceneCount int
}

// Status returns the section status of a viewer.
func (s *Service) Status(ctx context.Context, v Viewer) (Status, error) {
	h, blur, err := s.prefs(ctx, v.UserID)
	if err != nil {
		return Status{}, err
	}
	st := Status{PinSet: h != "", Blur: blur}
	st.Unlocked = !st.PinSet || s.isUnlocked(v.SessionID)
	keys, _ := s.LoadKeys(ctx)
	st.MetadataConfigured = keys.TPDB != "" || keys.StashDB != ""
	rows, err := s.Pool.Query(ctx, `SELECT l.id, l.name, (SELECT count(*)::int FROM adult_scenes a WHERE a.library_id = l.id)
		FROM libraries l WHERE l.kind = 'adult' ORDER BY lower(l.name)`)
	if err != nil {
		return st, err
	}
	defer rows.Close()
	for rows.Next() {
		var l LibraryInfo
		if err := rows.Scan(&l.ID, &l.Name, &l.SceneCount); err != nil {
			return st, err
		}
		st.Libraries = append(st.Libraries, l)
	}
	return st, rows.Err()
}

var pinRe = regexp.MustCompile(`^\d{4,12}$`)

// Unlock opens the section for the viewer's session when the PIN matches.
func (s *Service) Unlock(ctx context.Context, v Viewer, pin string) error {
	h, _, err := s.prefs(ctx, v.UserID)
	if err != nil {
		return err
	}
	if h == "" {
		s.setUnlocked(v.SessionID, true)
		return nil
	}
	if ok, err := auth.VerifyPassword(pin, h); err != nil || !ok {
		return ErrInvalidPIN
	}
	s.setUnlocked(v.SessionID, true)
	return nil
}

// Lock closes the section for the viewer's session.
func (s *Service) Lock(v Viewer) { s.setUnlocked(v.SessionID, false) }

// PrefsInput changes blurring and the PIN (nil = unchanged; PIN "" = remove).
type PrefsInput struct {
	Blur       *bool
	PIN        *string
	CurrentPIN string
}

// SetPrefs saves the viewer's preferences.
func (s *Service) SetPrefs(ctx context.Context, v Viewer, in PrefsInput) error {
	h, blur, err := s.prefs(ctx, v.UserID)
	if err != nil {
		return err
	}
	if in.Blur != nil {
		blur = *in.Blur
	}
	if in.PIN != nil {
		if h != "" {
			if ok, err := auth.VerifyPassword(in.CurrentPIN, h); err != nil || !ok {
				return ErrInvalidPIN
			}
		}
		if *in.PIN == "" {
			h = ""
		} else {
			if !pinRe.MatchString(*in.PIN) {
				return ErrBadPIN
			}
			if h, err = auth.HashPassword(*in.PIN); err != nil {
				return err
			}
			// The session that set the PIN stays open.
			s.setUnlocked(v.SessionID, true)
		}
	}
	var hp *string
	if h != "" {
		hp = &h
	}
	_, err = s.Pool.Exec(ctx, `INSERT INTO adult_prefs (user_id, blur, pin_hash) VALUES ($1, $2, $3)
		ON CONFLICT (user_id) DO UPDATE SET blur = EXCLUDED.blur, pin_hash = EXCLUDED.pin_hash`, v.UserID, blur, hp)
	return err
}

// SetAccess gives or takes away adult access (admin; confirm is required to give it).
func (s *Service) SetAccess(ctx context.Context, actor auth.Principal, userID uuid.UUID, enabled, confirm bool) error {
	var exists bool
	if err := s.Pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE id = $1)`, userID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return ErrNotFound
	}
	if !enabled {
		if _, err := s.Pool.Exec(ctx, `DELETE FROM adult_access WHERE user_id = $1`, userID); err != nil {
			return err
		}
		// Open sessions of that account lose access at once (Access reads the table
		// on every request); its PIN unlocks are irrelevant without access.
		return s.audit(ctx, actor.User.ID, "adult_access_revoked", userID)
	}
	if !confirm {
		return ErrConfirm
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO adult_access (user_id, granted_by) VALUES ($1, $2) ON CONFLICT (user_id) DO NOTHING`,
		userID, actor.User.ID); err != nil {
		return err
	}
	return s.audit(ctx, actor.User.ID, "adult_access_granted", userID)
}

func (s *Service) audit(ctx context.Context, actor uuid.UUID, action string, target uuid.UUID) error {
	_, err := s.Pool.Exec(ctx, `INSERT INTO audit_log (user_id, action, detail) VALUES ($1, $2, jsonb_build_object('user_id', $3::text))`,
		actor, action, target.String())
	return err
}
