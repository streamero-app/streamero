package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/netx"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// Policy constants.
const (
	MaxFailedLogins  = 5
	LockoutDuration  = 15 * time.Minute
	SessionTTL       = 30 * 24 * time.Hour // sliding
	touchInterval    = time.Minute         // how often last_seen_at is written
	maxSetupFailures = 10
	setupCooldown    = time.Minute
)

// Errors returned by the service; the API layer maps them to HTTP statuses.
var (
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrSetupDone          = errors.New("setup has already been completed")
	ErrBadSetupCode       = errors.New("invalid setup code")
	ErrSetupThrottled     = errors.New("too many wrong setup codes; try again in a minute")
	ErrUsernameTaken      = errors.New("username is already taken")
	ErrInvalidUsername    = errors.New("username may contain letters, digits, spaces and . _ - @")
	ErrNotFound           = errors.New("not found")
	ErrLastAdmin          = errors.New("the last enabled admin cannot be removed, demoted or disabled")
	ErrSelf               = errors.New("you cannot delete or disable your own account")
	ErrUnauthenticated    = errors.New("not signed in")
	ErrInvalidName        = errors.New("a name is required")
	ErrSessionRequired    = errors.New("this action needs a signed-in session, not an API key")
	ErrTOTPRequired       = errors.New("two-factor code required")
	ErrInvalidTOTP        = errors.New("invalid two-factor code")
	ErrTOTPAlreadyOn      = errors.New("two-factor authentication is already enabled")
	ErrTOTPNotStarted     = errors.New("start two-factor setup first")
)

// LockedError is returned while an account is locked after failed logins.
type LockedError struct{ Until time.Time }

func (e *LockedError) Error() string {
	return fmt.Sprintf("account locked after %d failed sign-ins; try again later", MaxFailedLogins)
}

// User is an account.
type User struct {
	ID          uuid.UUID
	Username    string
	IsAdmin     bool
	Disabled    bool
	TOTPEnabled bool
	Permissions Permissions
	CreatedAt   time.Time
	LastLoginAt *time.Time
}

// totpEnabledSQL is a column expression for users aliased as alias.
func totpEnabledSQL(alias string) string {
	return `EXISTS (SELECT 1 FROM user_totp t WHERE t.user_id = ` + alias + `.id AND t.enabled)`
}

// Session is a signed-in device.
type Session struct {
	ID         uuid.UUID
	UserID     uuid.UUID
	Username   string
	DeviceName string
	Client     string
	CreatedAt  time.Time
	LastSeenAt time.Time
}

// Principal is the authenticated caller: a user session or an API key.
type Principal struct {
	User      User
	SessionID uuid.UUID // zero for API keys
	APIKeyID  uuid.UUID // zero for sessions
	Profile   *Profile  // nil = main profile (the account itself)
}

// Device describes the client creating a session.
type Device struct{ Name, Client string }

// Service implements accounts and sessions on top of Postgres.
type Service struct {
	pool      *pgxpool.Pool
	setupCode string
	now       func() time.Time

	mu            sync.Mutex
	setupFailures int
	setupBlocked  time.Time
}

// NewService returns a Service. setupCode guards the first-run setup.
func NewService(pool *pgxpool.Pool, setupCode string) *Service {
	return &Service{pool: pool, setupCode: setupCode, now: time.Now}
}

var usernameRe = regexp.MustCompile(`^[\p{L}\p{N}._@\- ]{1,64}$`)

// NormalizeUsername trims and validates a username.
func NormalizeUsername(s string) (string, error) {
	s = strings.TrimSpace(s)
	if !usernameRe.MatchString(s) {
		return "", ErrInvalidUsername
	}
	return s, nil
}

func (s *Service) checkSetupCode(code string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.now()
	if now.Before(s.setupBlocked) {
		return ErrSetupThrottled
	}
	if setupCodeMatches(code, s.setupCode) {
		s.setupFailures = 0
		return nil
	}
	s.setupFailures++
	if s.setupFailures >= maxSetupFailures {
		s.setupFailures = 0
		s.setupBlocked = now.Add(setupCooldown)
	}
	return ErrBadSetupCode
}

// Setup creates the first admin, marks setup complete and signs the admin in.
func (s *Service) Setup(ctx context.Context, code, serverName, username, password string, dev Device) (Principal, string, error) {
	var done bool
	if _, err := settings.New(s.pool).Get(ctx, settings.KeySetupCompleted, &done); err != nil {
		return Principal{}, "", err
	}
	if done {
		return Principal{}, "", ErrSetupDone
	}
	if err := s.checkSetupCode(code); err != nil {
		return Principal{}, "", err
	}
	username, err := NormalizeUsername(username)
	if err != nil {
		return Principal{}, "", err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return Principal{}, "", err
	}
	var p Principal
	var token string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		// Claiming the setup flag first makes concurrent setups race-free.
		tag, err := tx.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, 'true')
			ON CONFLICT (key) DO UPDATE SET value = 'true', updated_at = now() WHERE settings.value <> 'true'`,
			settings.KeySetupCompleted)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrSetupDone
		}
		if name := strings.TrimSpace(serverName); name != "" {
			raw, _ := json.Marshal(name)
			if _, err := tx.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()`,
				settings.KeyServerName, raw); err != nil {
				return err
			}
		}
		u, err := insertUser(ctx, tx, username, hash, true, nil)
		if err != nil {
			return err
		}
		p, token, err = createSession(ctx, tx, u, dev, s.now())
		if err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "setup.completed", map[string]any{"username": u.Username})
	})
	return p, token, err
}

// Login verifies credentials and creates a session.
func (s *Service) Login(ctx context.Context, username, password, totpCode string, dev Device) (Principal, string, error) {
	username = strings.TrimSpace(username)
	var (
		u           User
		hash        string
		lockedUntil *time.Time
		perms       []byte
	)
	err := s.pool.QueryRow(ctx, `SELECT id, username, password_hash, is_admin, disabled, `+totpEnabledSQL("users")+`,
			created_at, last_login_at, locked_until, permissions
		FROM users WHERE lower(username) = lower($1)`, username).
		Scan(&u.ID, &u.Username, &hash, &u.IsAdmin, &u.Disabled, &u.TOTPEnabled, &u.CreatedAt, &u.LastLoginAt, &lockedUntil, &perms)
	if errors.Is(err, pgx.ErrNoRows) {
		_, _ = VerifyPassword(password, dummyHash) // equalize timing
		_ = audit(ctx, s.pool, nil, "login.failed", map[string]any{"username": username, "reason": "unknown_user"})
		return Principal{}, "", ErrInvalidCredentials
	}
	if err != nil {
		return Principal{}, "", err
	}
	u.Permissions = parsePermissions(perms)
	now := s.now()
	if lockedUntil != nil && now.Before(*lockedUntil) {
		return Principal{}, "", &LockedError{Until: *lockedUntil}
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return Principal{}, "", err
	}
	if !ok {
		return Principal{}, "", s.recordFailedLogin(ctx, u.ID, now, "bad_password", ErrInvalidCredentials)
	}
	if u.Disabled {
		_ = audit(ctx, s.pool, &u.ID, "login.failed", map[string]any{"reason": "disabled"})
		return Principal{}, "", ErrInvalidCredentials
	}
	if u.TOTPEnabled {
		if strings.TrimSpace(totpCode) == "" {
			return Principal{}, "", ErrTOTPRequired
		}
		ok, err := s.checkSecondFactor(ctx, u.ID, totpCode, now)
		if err != nil {
			return Principal{}, "", err
		}
		if !ok {
			return Principal{}, "", s.recordFailedLogin(ctx, u.ID, now, "bad_totp", ErrInvalidTOTP)
		}
	}
	var p Principal
	var token string
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET failed_logins = 0, locked_until = NULL, last_login_at = $2 WHERE id = $1`,
			u.ID, now); err != nil {
			return err
		}
		u.LastLoginAt = &now
		p, token, err = createSession(ctx, tx, u, dev, now)
		if err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "login.succeeded", map[string]any{"session_id": p.SessionID, "device": dev.Name})
	})
	return p, token, err
}

// recordFailedLogin counts a failure (atomically, so parallel guesses cannot
// exceed the limit) and returns LockedError when the limit is reached, else fail.
func (s *Service) recordFailedLogin(ctx context.Context, userID uuid.UUID, now time.Time, reason string, fail error) error {
	var lock *time.Time
	if err := s.pool.QueryRow(ctx, `UPDATE users SET
			locked_until  = CASE WHEN failed_logins + 1 >= $2 THEN $3 ELSE locked_until END,
			failed_logins = CASE WHEN failed_logins + 1 >= $2 THEN 0 ELSE failed_logins + 1 END
		WHERE id = $1 RETURNING CASE WHEN locked_until > $4 THEN locked_until END`,
		userID, MaxFailedLogins, now.Add(LockoutDuration), now).Scan(&lock); err != nil {
		return err
	}
	_ = audit(ctx, s.pool, &userID, "login.failed", map[string]any{"reason": reason, "locked": lock != nil})
	if lock != nil {
		return &LockedError{Until: *lock}
	}
	return fail
}

// Authenticate resolves a session token. Sessions slide: each use extends expiry.
func (s *Service) Authenticate(ctx context.Context, token string) (Principal, error) {
	if token == "" {
		return Principal{}, ErrUnauthenticated
	}
	if isAPIKey(token) {
		return s.authenticateAPIKey(ctx, token)
	}
	var p Principal
	var lastSeen time.Time
	var perms []byte
	err := s.pool.QueryRow(ctx, `SELECT s.id, s.last_seen_at, u.id, u.username, u.is_admin, u.disabled, `+totpEnabledSQL("u")+`,
			u.created_at, u.last_login_at, u.permissions
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.token_hash = $1 AND s.expires_at > now() AND NOT u.disabled`, hashToken(token)).
		Scan(&p.SessionID, &lastSeen, &p.User.ID, &p.User.Username, &p.User.IsAdmin, &p.User.Disabled, &p.User.TOTPEnabled,
			&p.User.CreatedAt, &p.User.LastLoginAt, &perms)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, ErrUnauthenticated
	}
	if err != nil {
		return Principal{}, err
	}
	p.User.Permissions = parsePermissions(perms)
	if p.Profile, err = s.loadProfile(ctx, p.SessionID); err != nil {
		return Principal{}, err
	}
	if now := s.now(); now.Sub(lastSeen) > touchInterval {
		_, _ = s.pool.Exec(ctx, `UPDATE sessions SET last_seen_at = $2, expires_at = $3 WHERE id = $1`,
			p.SessionID, now, now.Add(SessionTTL))
	}
	return p, nil
}

// Logout deletes the caller's session.
func (s *Service) Logout(ctx context.Context, p Principal) error {
	if p.SessionID == uuid.Nil {
		return ErrSessionRequired
	}
	if _, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1`, p.SessionID); err != nil {
		return err
	}
	return audit(ctx, s.pool, &p.User.ID, "logout", map[string]any{"session_id": p.SessionID})
}

// ChangePassword changes the caller's password and signs out their other sessions.
func (s *Service) ChangePassword(ctx context.Context, p Principal, current, next string) error {
	if p.SessionID == uuid.Nil {
		return ErrSessionRequired
	}
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.User.ID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(current, hash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	newHash, err := HashPassword(next)
	if err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `UPDATE users SET password_hash = $2 WHERE id = $1`, p.User.ID, newHash); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, p.User.ID, p.SessionID); err != nil {
			return err
		}
		return audit(ctx, tx, &p.User.ID, "password.changed", nil)
	})
}

var userCols = `id, username, is_admin, disabled, ` + totpEnabledSQL("users") + `, created_at, last_login_at, permissions`

func scanUser(row pgx.Row) (User, error) {
	var u User
	var perms []byte
	err := row.Scan(&u.ID, &u.Username, &u.IsAdmin, &u.Disabled, &u.TOTPEnabled, &u.CreatedAt, &u.LastLoginAt, &perms)
	u.Permissions = parsePermissions(perms)
	if errors.Is(err, pgx.ErrNoRows) {
		return u, ErrNotFound
	}
	return u, err
}

// ListUsers returns all users ordered by name.
func (s *Service) ListUsers(ctx context.Context) ([]User, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+userCols+` FROM users ORDER BY lower(username)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []User
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

// CreateUser adds a user.
func (s *Service) CreateUser(ctx context.Context, actor Principal, username, password string, isAdmin bool, perms *Permissions) (User, error) {
	username, err := NormalizeUsername(username)
	if err != nil {
		return User{}, err
	}
	hash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}
	var u User
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err = insertUser(ctx, tx, username, hash, isAdmin, perms)
		if err != nil {
			return err
		}
		return audit(ctx, tx, &actor.User.ID, "user.created", map[string]any{"user_id": u.ID, "username": u.Username, "is_admin": isAdmin})
	})
	return u, err
}

// UserUpdate holds optional changes to a user.
type UserUpdate struct {
	IsAdmin        *bool
	Disabled       *bool
	Password       *string
	Permissions    *Permissions
	ResetTwoFactor bool
}

// UpdateUser changes a user. Disabling or setting a password signs the user out everywhere.
func (s *Service) UpdateUser(ctx context.Context, actor Principal, id uuid.UUID, upd UserUpdate) (User, error) {
	var hash *string
	if upd.Password != nil {
		h, err := HashPassword(*upd.Password)
		if err != nil {
			return User{}, err
		}
		hash = &h
	}
	if id == actor.User.ID && upd.Disabled != nil && *upd.Disabled {
		return User{}, ErrSelf
	}
	var permsJSON []byte
	if upd.Permissions != nil {
		var err error
		if permsJSON, err = json.Marshal(upd.Permissions); err != nil {
			return User{}, err
		}
	}
	var u User
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var err error
		if u, err = scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 FOR UPDATE`, id)); err != nil {
			return err
		}
		demote := upd.IsAdmin != nil && !*upd.IsAdmin && u.IsAdmin
		disable := upd.Disabled != nil && *upd.Disabled && !u.Disabled && u.IsAdmin
		if demote || disable {
			if err := ensureOtherAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		u, err = scanUser(tx.QueryRow(ctx, `UPDATE users SET
				is_admin = COALESCE($2, is_admin),
				disabled = COALESCE($3, disabled),
				password_hash = COALESCE($4, password_hash),
				failed_logins = CASE WHEN $4::text IS NULL THEN failed_logins ELSE 0 END,
				locked_until = CASE WHEN $4::text IS NULL THEN locked_until ELSE NULL END,
				permissions = COALESCE($5::jsonb, permissions)
			WHERE id = $1 RETURNING `+userCols, id, upd.IsAdmin, upd.Disabled, hash, permsJSON))
		if err != nil {
			return err
		}
		if err := enforceMaxSessions(ctx, tx, u, uuid.Nil); err != nil {
			return err
		}
		if upd.ResetTwoFactor {
			if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id = $1`, id); err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, id); err != nil {
				return err
			}
			u.TOTPEnabled = false
		}
		if (upd.Disabled != nil && *upd.Disabled) || hash != nil || upd.ResetTwoFactor {
			if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1`, id); err != nil {
				return err
			}
		}
		return audit(ctx, tx, &actor.User.ID, "user.updated", map[string]any{
			"user_id": id, "is_admin": upd.IsAdmin, "disabled": upd.Disabled, "password_reset": hash != nil,
			"two_factor_reset": upd.ResetTwoFactor, "permissions_changed": upd.Permissions != nil})
	})
	return u, err
}

// DeleteUser removes a user and their sessions.
func (s *Service) DeleteUser(ctx context.Context, actor Principal, id uuid.UUID) error {
	if id == actor.User.ID {
		return ErrSelf
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 FOR UPDATE`, id))
		if err != nil {
			return err
		}
		if u.IsAdmin {
			if err := ensureOtherAdmin(ctx, tx, id); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM users WHERE id = $1`, id); err != nil {
			return err
		}
		return audit(ctx, tx, &actor.User.ID, "user.deleted", map[string]any{"user_id": id, "username": u.Username})
	})
}

// ListSessions returns the sessions of userID, or of everyone when userID is nil.
func (s *Service) ListSessions(ctx context.Context, userID *uuid.UUID) ([]Session, error) {
	rows, err := s.pool.Query(ctx, `SELECT s.id, s.user_id, u.username, s.device_name, s.client, s.created_at, s.last_seen_at
		FROM sessions s JOIN users u ON u.id = s.user_id
		WHERE s.expires_at > now() AND ($1::uuid IS NULL OR s.user_id = $1)
		ORDER BY s.last_seen_at DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Session
	for rows.Next() {
		var x Session
		if err := rows.Scan(&x.ID, &x.UserID, &x.Username, &x.DeviceName, &x.Client, &x.CreatedAt, &x.LastSeenAt); err != nil {
			return nil, err
		}
		out = append(out, x)
	}
	return out, rows.Err()
}

// RevokeSession deletes a session. Non-admins may only revoke their own.
func (s *Service) RevokeSession(ctx context.Context, actor Principal, id uuid.UUID) error {
	var owner *uuid.UUID
	if !actor.User.IsAdmin {
		owner = &actor.User.ID
	}
	tag, err := s.pool.Exec(ctx, `DELETE FROM sessions WHERE id = $1 AND ($2::uuid IS NULL OR user_id = $2)`, id, owner)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return audit(ctx, s.pool, &actor.User.ID, "session.revoked", map[string]any{"session_id": id})
}

type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
}

// Audit records an administrative action in the activity log.
func (s *Service) Audit(ctx context.Context, userID *uuid.UUID, action string, detail map[string]any) error {
	return audit(ctx, s.pool, userID, action, detail)
}

func audit(ctx context.Context, db execer, userID *uuid.UUID, action string, detail map[string]any) error {
	if detail == nil {
		detail = map[string]any{}
	}
	if ip := netx.FromContext(ctx); ip.IsValid() {
		detail["ip"] = ip.String()
	}
	raw, err := json.Marshal(detail)
	if err != nil {
		return err
	}
	_, err = db.Exec(ctx, `INSERT INTO audit_log (user_id, action, detail) VALUES ($1, $2, $3)`, userID, action, raw)
	return err
}

func insertUser(ctx context.Context, tx pgx.Tx, username, hash string, isAdmin bool, perms *Permissions) (User, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return User{}, err
	}
	p := DefaultPermissions()
	if perms != nil {
		p = *perms
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return User{}, err
	}
	u, err := scanUser(tx.QueryRow(ctx, `INSERT INTO users (id, username, password_hash, is_admin, permissions)
		VALUES ($1, $2, $3, $4, $5) RETURNING `+userCols, id, username, hash, isAdmin, raw))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return User{}, ErrUsernameTaken
	}
	return u, err
}

func createSession(ctx context.Context, tx pgx.Tx, u User, dev Device, now time.Time) (Principal, string, error) {
	id, err := uuid.NewV7()
	if err != nil {
		return Principal{}, "", err
	}
	token, hash, err := newToken()
	if err != nil {
		return Principal{}, "", err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO sessions (id, user_id, token_hash, device_name, client, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6, $7)`, id, u.ID, hash, truncate(dev.Name, 100), truncate(dev.Client, 100),
		now, now.Add(SessionTTL)); err != nil {
		return Principal{}, "", err
	}
	if err := enforceMaxSessions(ctx, tx, u, id); err != nil {
		return Principal{}, "", err
	}
	return Principal{User: u, SessionID: id}, token, nil
}

// ensureOtherAdmin fails unless an enabled admin other than id exists.
func ensureOtherAdmin(ctx context.Context, tx pgx.Tx, id uuid.UUID) error {
	// Lock all enabled admins so two concurrent demotions cannot both succeed.
	rows, err := tx.Query(ctx, `SELECT id FROM users WHERE is_admin AND NOT disabled FOR UPDATE`)
	if err != nil {
		return err
	}
	defer rows.Close()
	others := 0
	for rows.Next() {
		var other uuid.UUID
		if err := rows.Scan(&other); err != nil {
			return err
		}
		if other != id {
			others++
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if others == 0 {
		return ErrLastAdmin
	}
	return nil
}

func truncate(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > n {
		r = r[:n]
	}
	return string(r)
}

// enforceMaxSessions signs out the least recently used sessions beyond the user's
// limit, always keeping keep (the session being created, if any). Admins are exempt.
func enforceMaxSessions(ctx context.Context, tx pgx.Tx, u User, keep uuid.UUID) error {
	if u.IsAdmin || u.Permissions.MaxSessions <= 0 {
		return nil
	}
	_, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id NOT IN (
		SELECT id FROM sessions WHERE user_id = $1 ORDER BY (id = $2) DESC, last_seen_at DESC LIMIT $3)`,
		u.ID, keep, u.Permissions.MaxSessions)
	return err
}
