package auth

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Profile limits and PIN throttling.
const (
	MaxProfiles    = 6
	maxPinFailures = 5
	pinLockout     = 5 * time.Minute
)

// Profile errors.
var (
	ErrPinRequired     = errors.New("this profile is protected by a PIN")
	ErrInvalidPin      = errors.New("wrong PIN")
	ErrProfileLocked   = errors.New("too many wrong PINs; try again in a few minutes")
	ErrKidProfile      = errors.New("not available in a kids profile")
	ErrTooManyProfiles = errors.New("an account can have at most 6 profiles")
)

// Profile is a profile inside an account.
type Profile struct {
	ID        uuid.UUID
	Name      string
	Color     string
	HasPin    bool
	IsKid     bool
	MaxRating *int
	HasAvatar bool
}

// ProfileInput holds the editable fields of a profile.
type ProfileInput struct {
	Name      string
	Color     string
	IsKid     bool
	MaxRating *int
	Pin       *string // set/change
	RemovePin bool
}

// IsKid reports whether the caller is using a kids profile.
func (p Principal) IsKid() bool { return p.Profile != nil && p.Profile.IsKid }

const profileCols = `id, name, color, pin_hash IS NOT NULL, is_kid, max_rating, has_avatar`

func scanProfile(row pgx.Row) (Profile, error) {
	var pr Profile
	err := row.Scan(&pr.ID, &pr.Name, &pr.Color, &pr.HasPin, &pr.IsKid, &pr.MaxRating, &pr.HasAvatar)
	if errors.Is(err, pgx.ErrNoRows) {
		return pr, ErrNotFound
	}
	return pr, err
}

// ListProfiles returns the caller's profiles in creation order.
func (s *Service) ListProfiles(ctx context.Context, p Principal) ([]Profile, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+profileCols+` FROM profiles WHERE user_id = $1 ORDER BY created_at`, p.User.ID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Profile
	for rows.Next() {
		pr, err := scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, pr)
	}
	return out, rows.Err()
}

func pinHash(in ProfileInput) (*string, error) {
	if in.Pin == nil {
		return nil, nil
	}
	h, err := HashPassword(*in.Pin)
	return &h, err
}

// CreateProfile adds a profile to the caller's account.
func (s *Service) CreateProfile(ctx context.Context, p Principal, in ProfileInput) (Profile, error) {
	if p.IsKid() {
		return Profile{}, ErrKidProfile
	}
	name := truncate(in.Name, 40)
	if name == "" {
		return Profile{}, ErrInvalidName
	}
	hash, err := pinHash(in)
	if err != nil {
		return Profile{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return Profile{}, err
	}
	var pr Profile
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Lock the user row so concurrent creates cannot exceed the limit.
		if _, err := tx.Exec(ctx, `SELECT 1 FROM users WHERE id = $1 FOR UPDATE`, p.User.ID); err != nil {
			return err
		}
		var n int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM profiles WHERE user_id = $1`, p.User.ID).Scan(&n); err != nil {
			return err
		}
		if n >= MaxProfiles {
			return ErrTooManyProfiles
		}
		pr, err = scanProfile(tx.QueryRow(ctx, `INSERT INTO profiles (id, user_id, name, color, pin_hash, is_kid, max_rating)
			VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING `+profileCols, id, p.User.ID, name, strings.ToLower(in.Color), hash, in.IsKid, in.MaxRating))
		return err
	})
	return pr, err
}

// UpdateProfile changes one of the caller's profiles.
func (s *Service) UpdateProfile(ctx context.Context, p Principal, id uuid.UUID, in ProfileInput) (Profile, error) {
	if p.IsKid() {
		return Profile{}, ErrKidProfile
	}
	name := truncate(in.Name, 40)
	if name == "" {
		return Profile{}, ErrInvalidName
	}
	hash, err := pinHash(in)
	if err != nil {
		return Profile{}, err
	}
	return scanProfile(s.pool.QueryRow(ctx, `UPDATE profiles SET name = $3, color = $4, is_kid = $5, max_rating = $6,
			pin_hash = CASE WHEN $8 THEN NULL ELSE COALESCE($7, pin_hash) END,
			failed_pins = 0, locked_until = NULL
		WHERE id = $1 AND user_id = $2 RETURNING `+profileCols,
		id, p.User.ID, name, strings.ToLower(in.Color), in.IsKid, in.MaxRating, hash, in.RemovePin))
}

// DeleteProfile removes one of the caller's profiles (sessions using it fall back to the main profile).
func (s *Service) DeleteProfile(ctx context.Context, p Principal, id uuid.UUID) error {
	if p.IsKid() {
		return ErrKidProfile
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM profiles WHERE id = $1 AND user_id = $2`, id, p.User.ID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SelectProfile switches the caller's session to a profile, checking its PIN.
func (s *Service) SelectProfile(ctx context.Context, p Principal, id uuid.UUID, pin string) error {
	if p.SessionID == uuid.Nil {
		return ErrSessionRequired
	}
	var hash *string
	var locked *time.Time
	err := s.pool.QueryRow(ctx, `SELECT pin_hash, locked_until FROM profiles WHERE id = $1 AND user_id = $2`, id, p.User.ID).
		Scan(&hash, &locked)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	now := s.now()
	// From a kids profile, switching to another protected profile still needs its PIN (checked below).
	if hash != nil {
		if locked != nil && now.Before(*locked) {
			return ErrProfileLocked
		}
		if pin == "" {
			return ErrPinRequired
		}
		ok, err := VerifyPassword(pin, *hash)
		if err != nil {
			return err
		}
		if !ok {
			var lock *time.Time
			if err := s.pool.QueryRow(ctx, `UPDATE profiles SET
					locked_until = CASE WHEN failed_pins + 1 >= $2 THEN $3 ELSE locked_until END,
					failed_pins  = CASE WHEN failed_pins + 1 >= $2 THEN 0 ELSE failed_pins + 1 END
				WHERE id = $1 RETURNING CASE WHEN locked_until > $4 THEN locked_until END`,
				id, maxPinFailures, now.Add(pinLockout), now).Scan(&lock); err != nil {
				return err
			}
			if lock != nil {
				return ErrProfileLocked
			}
			return ErrInvalidPin
		}
		if _, err := s.pool.Exec(ctx, `UPDATE profiles SET failed_pins = 0, locked_until = NULL WHERE id = $1`, id); err != nil {
			return err
		}
	} else if p.IsKid() {
		// Leaving a kids profile for an unprotected adult profile would bypass the account password.
		var isKid bool
		if err := s.pool.QueryRow(ctx, `SELECT is_kid FROM profiles WHERE id = $1`, id).Scan(&isKid); err != nil {
			return err
		}
		if !isKid {
			return ErrKidProfile
		}
	}
	_, err = s.pool.Exec(ctx, `UPDATE sessions SET profile_id = $2 WHERE id = $1`, p.SessionID, id)
	return err
}

// SelectMainProfile switches back to the account itself. From a kids profile
// this needs the account password.
func (s *Service) SelectMainProfile(ctx context.Context, p Principal, password string) error {
	if p.SessionID == uuid.Nil {
		return ErrSessionRequired
	}
	if p.IsKid() {
		var hash string
		if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.User.ID).Scan(&hash); err != nil {
			return err
		}
		ok, err := VerifyPassword(password, hash)
		if err != nil {
			return err
		}
		if !ok {
			return s.recordFailedLogin(ctx, p.User.ID, s.now(), "bad_password_profile_switch", ErrInvalidCredentials)
		}
	}
	_, err := s.pool.Exec(ctx, `UPDATE sessions SET profile_id = NULL WHERE id = $1`, p.SessionID)
	return err
}

// loadProfile fills the session's current profile, if any.
func (s *Service) loadProfile(ctx context.Context, sessionID uuid.UUID) (*Profile, error) {
	pr, err := scanProfile(s.pool.QueryRow(ctx, `SELECT `+profileCols+` FROM profiles
		WHERE id = (SELECT profile_id FROM sessions WHERE id = $1)`, sessionID))
	if errors.Is(err, ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &pr, nil
}

// ProfilesOf returns the profiles of any user (for administrators choosing who
// a sign-in-less client such as DLNA acts as).
func (s *Service) ProfilesOf(ctx context.Context, userID uuid.UUID) ([]Profile, error) {
	return s.ListProfiles(ctx, Principal{User: User{ID: userID}})
}

// ActAs builds the principal of a user and, optionally, one of its profiles,
// for clients without sign-in (DLNA). A disabled or deleted user, or a
// profile of another user, is ErrNotFound.
func (s *Service) ActAs(ctx context.Context, userID uuid.UUID, profileID *uuid.UUID) (Principal, error) {
	u, err := scanUser(s.pool.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 AND NOT disabled`, userID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return Principal{}, ErrNotFound
		}
		return Principal{}, err
	}
	p := Principal{User: u}
	if profileID != nil {
		pr, err := scanProfile(s.pool.QueryRow(ctx, `SELECT `+profileCols+` FROM profiles WHERE id = $1 AND user_id = $2`, *profileID, userID))
		if err != nil {
			return Principal{}, err
		}
		p.Profile = &pr
	}
	return p, nil
}
