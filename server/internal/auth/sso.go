package auth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/settings"
)

// Settings keys of the sign-in configuration (D-037).
const (
	keyAuthGeneral = "auth.general"
	keyAuthLDAP    = "auth.ldap"
)

// Errors of single sign-on and directory sign-in.
var (
	ErrPasswordLoginDisabled = errors.New("password sign-in is turned off; use single sign-on")
	ErrNoLocalPassword       = errors.New("this account has no Streamero password")
	ErrNoAccount             = errors.New("no Streamero account is linked to this sign-in")
	ErrNotAllowed            = errors.New("this account is not allowed to sign in here")
	ErrAccountDisabled       = errors.New("this account is disabled")
	ErrIdentityTaken         = errors.New("this external account is already linked to another user")
	ErrAlreadyLinked         = errors.New("an account of this provider is already linked")
	ErrLastSignInMethod      = errors.New("this is the account's only way to sign in")
	ErrNoSSOAdmin            = errors.New("no enabled administrator can sign in with single sign-on or LDAP yet")
	ErrInvalidState          = errors.New("the sign-in expired or was already used; try again")
	ErrProviderFailed        = errors.New("the identity provider refused the sign-in")
)

// InvalidSettingError reports a rejected sign-in setting.
type InvalidSettingError struct{ Msg string }

func (e *InvalidSettingError) Error() string { return e.Msg }

func invalid(msg string) error { return &InvalidSettingError{Msg: msg} }

// SignInSettings are the general sign-in settings.
type SignInSettings struct {
	PublicURL            string `json:"publicUrl"`
	DisablePasswordLogin bool   `json:"disablePasswordLogin"`
}

func (s *Service) httpClient() *http.Client {
	if s.HTTPClient != nil {
		return s.HTTPClient
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// SignInSettings returns the general sign-in settings.
func (s *Service) SignInSettings(ctx context.Context) (SignInSettings, error) {
	var a SignInSettings
	_, err := settings.New(s.pool).Get(ctx, keyAuthGeneral, &a)
	return a, err
}

// NormalizePublicURL validates the server's public address ("" is allowed).
func NormalizePublicURL(raw string) (string, error) {
	raw = strings.TrimRight(strings.TrimSpace(raw), "/")
	if raw == "" {
		return "", nil
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" || u.User != nil ||
		u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("the public address must be an http(s) URL such as https://media.example.com")
	}
	return u.Scheme + "://" + u.Host + strings.TrimRight(u.EscapedPath(), "/"), nil
}

// UpdateAuthSettings saves the general sign-in settings. Password sign-in can only be
// turned off while an enabled administrator has another way in (SSO or LDAP).
func (s *Service) UpdateAuthSettings(ctx context.Context, actor Principal, in SignInSettings) (SignInSettings, error) {
	var err error
	if in.PublicURL, err = NormalizePublicURL(in.PublicURL); err != nil {
		return SignInSettings{}, err
	}
	if in.DisablePasswordLogin {
		ok, err := s.adminCanUseExternalSignIn(ctx)
		if err != nil {
			return SignInSettings{}, err
		}
		if !ok {
			return SignInSettings{}, ErrNoSSOAdmin
		}
	}
	if err := settings.New(s.pool).Set(ctx, keyAuthGeneral, in); err != nil {
		return SignInSettings{}, err
	}
	_ = s.Audit(ctx, &actor.User.ID, "auth.settings_changed", map[string]any{
		"public_url": in.PublicURL, "disable_password_login": in.DisablePasswordLogin})
	return in, nil
}

// adminCanUseExternalSignIn reports whether an enabled admin has a linked identity of
// an enabled OIDC provider or (with LDAP on) a directory identity.
func (s *Service) adminCanUseExternalSignIn(ctx context.Context) (bool, error) {
	l, err := s.LDAPSettings(ctx)
	if err != nil {
		return false, err
	}
	return s.externalAdminExists(ctx, "", l.Enabled)
}

// passwordLoginAllowed reports whether a local password may be used by this user.
func (s *Service) passwordLoginAllowed(ctx context.Context, isAdmin bool) (bool, error) {
	a, err := s.SignInSettings(ctx)
	if err != nil {
		return false, err
	}
	return !a.DisablePasswordLogin || (s.PasswordRecovery && isAdmin), nil
}

// ProviderButton is an SSO provider shown on the login screen.
type ProviderButton struct{ ID, Label, AuthorizeURL string }

// LoginMethods lists what the login screen offers.
func (s *Service) LoginMethods(ctx context.Context) (password, ldap bool, buttons []ProviderButton, err error) {
	a, err := s.SignInSettings(ctx)
	if err != nil {
		return false, false, nil, err
	}
	l, err := s.LDAPSettings(ctx)
	if err != nil {
		return false, false, nil, err
	}
	ldap = l.Enabled
	password = !a.DisablePasswordLogin || s.PasswordRecovery || ldap
	if a.PublicURL == "" {
		return password, ldap, nil, nil // SSO needs the public address for its redirect URI
	}
	rows, err := s.pool.Query(ctx, `SELECT id, label FROM oidc_providers WHERE enabled ORDER BY lower(label), id`)
	if err != nil {
		return false, false, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var b ProviderButton
		if err := rows.Scan(&b.ID, &b.Label); err != nil {
			return false, false, nil, err
		}
		b.AuthorizeURL = a.PublicURL + "/api/v1/auth/oidc/" + b.ID + "/authorize"
		buttons = append(buttons, b)
	}
	return password, ldap, buttons, rows.Err()
}

// Identity is an external account linked to a user.
type Identity struct {
	ID            uuid.UUID
	Kind          string
	ProviderID    string
	ProviderLabel string
	Label         string
	CreatedAt     time.Time
	LastLoginAt   *time.Time
}

// Identities lists a user's linked accounts and whether they have a local password.
func (s *Service) Identities(ctx context.Context, userID uuid.UUID) (bool, []Identity, error) {
	var hasPassword bool
	if err := s.pool.QueryRow(ctx, `SELECT password_hash <> '' FROM users WHERE id = $1`, userID).Scan(&hasPassword); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil, ErrNotFound
		}
		return false, nil, err
	}
	rows, err := s.pool.Query(ctx, `SELECT i.id, i.kind, i.provider_id, COALESCE(p.label, CASE WHEN i.kind = 'ldap' THEN 'LDAP' ELSE i.provider_id END),
			i.label, i.created_at, i.last_login_at
		FROM user_identities i LEFT JOIN oidc_providers p ON i.kind = 'oidc' AND p.id = i.provider_id
		WHERE i.user_id = $1 ORDER BY i.created_at`, userID)
	if err != nil {
		return false, nil, err
	}
	defer rows.Close()
	var out []Identity
	for rows.Next() {
		var x Identity
		if err := rows.Scan(&x.ID, &x.Kind, &x.ProviderID, &x.ProviderLabel, &x.Label, &x.CreatedAt, &x.LastLoginAt); err != nil {
			return false, nil, err
		}
		out = append(out, x)
	}
	return hasPassword, out, rows.Err()
}

// UnlinkIdentity removes one of the caller's identities unless it is their only way in.
func (s *Service) UnlinkIdentity(ctx context.Context, p Principal, id uuid.UUID) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var hasPassword bool
		if err := tx.QueryRow(ctx, `SELECT password_hash <> '' FROM users WHERE id = $1 FOR UPDATE`, p.User.ID).Scan(&hasPassword); err != nil {
			return err
		}
		var others int
		if err := tx.QueryRow(ctx, `SELECT count(*) FROM user_identities WHERE user_id = $1 AND id <> $2`, p.User.ID, id).Scan(&others); err != nil {
			return err
		}
		if !hasPassword && others == 0 {
			return ErrLastSignInMethod
		}
		tag, err := tx.Exec(ctx, `DELETE FROM user_identities WHERE id = $1 AND user_id = $2`, id, p.User.ID)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit(ctx, tx, &p.User.ID, "identity.unlinked", map[string]any{"identity_id": id})
	})
}

// externalAccount is a user authenticated by an identity provider or directory.
type externalAccount struct {
	Kind, ProviderID, Subject, Label string
	Username, Email, DisplayName     string
	EmailVerified                    bool
	// IsAdmin is the admin status the provider grants (nil = managed in Streamero).
	IsAdmin     *bool
	LinkByEmail bool
	AutoCreate  bool
	// LinkUser links the identity to this signed-in user instead of signing in.
	LinkUser *uuid.UUID
}

// resolveExternal maps an external account to a user: an existing link, an explicit
// link request, a user with the same verified e-mail, or a new user. It also keeps
// the admin flag in sync with the provider (never demoting the last admin).
func (s *Service) resolveExternal(ctx context.Context, tx pgx.Tx, a externalAccount) (User, error) {
	var userID uuid.UUID
	err := tx.QueryRow(ctx, `UPDATE user_identities SET last_login_at = now(), label = $4
		WHERE kind = $1 AND provider_id = $2 AND subject = $3 RETURNING user_id`,
		a.Kind, a.ProviderID, a.Subject, a.Label).Scan(&userID)
	switch {
	case err == nil:
		if a.LinkUser != nil && *a.LinkUser != userID {
			return User{}, ErrIdentityTaken
		}
	case !errors.Is(err, pgx.ErrNoRows):
		return User{}, err
	case a.LinkUser != nil:
		userID = *a.LinkUser
		if err := s.insertIdentity(ctx, tx, userID, a); err != nil {
			return User{}, err
		}
	default:
		found := false
		if a.LinkByEmail && a.EmailVerified && a.Email != "" {
			rows, err := tx.Query(ctx, `SELECT id FROM users WHERE lower(email) = lower($1) LIMIT 2`, a.Email)
			if err != nil {
				return User{}, err
			}
			ids, err := pgx.CollectRows(rows, pgx.RowTo[uuid.UUID])
			if err != nil {
				return User{}, err
			}
			if len(ids) == 1 { // ambiguous addresses are never linked automatically
				userID, found = ids[0], true
			}
		}
		if !found {
			if !a.AutoCreate {
				return User{}, ErrNoAccount
			}
			u, err := s.createExternalUser(ctx, tx, a)
			if err != nil {
				return User{}, err
			}
			userID = u.ID
		}
		if err := s.insertIdentity(ctx, tx, userID, a); err != nil {
			return User{}, err
		}
	}
	u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 FOR UPDATE`, userID))
	if err != nil {
		return User{}, err
	}
	if u.Disabled {
		return User{}, ErrAccountDisabled
	}
	if a.IsAdmin != nil && *a.IsAdmin != u.IsAdmin {
		want := *a.IsAdmin
		if !want {
			if err := ensureOtherAdmin(ctx, tx, u.ID); errors.Is(err, ErrLastAdmin) {
				_ = audit(ctx, tx, &u.ID, "identity.admin_sync_skipped", map[string]any{"reason": "last_admin", "provider": a.Kind + ":" + a.ProviderID})
				want = true
			} else if err != nil {
				return User{}, err
			}
		}
		if want != u.IsAdmin {
			if _, err := tx.Exec(ctx, `UPDATE users SET is_admin = $2 WHERE id = $1`, u.ID, want); err != nil {
				return User{}, err
			}
			_ = audit(ctx, tx, &u.ID, "identity.admin_synced", map[string]any{"is_admin": want, "provider": a.Kind + ":" + a.ProviderID})
			u.IsAdmin = want
		}
	}
	return u, nil
}

func (s *Service) insertIdentity(ctx context.Context, tx pgx.Tx, userID uuid.UUID, a externalAccount) error {
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM user_identities WHERE user_id = $1 AND kind = $2 AND provider_id = $3)`,
		userID, a.Kind, a.ProviderID).Scan(&exists); err != nil {
		return err
	}
	if exists {
		return ErrAlreadyLinked
	}
	id, err := uuid.NewV7()
	if err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `INSERT INTO user_identities (id, user_id, kind, provider_id, subject, label, last_login_at)
		VALUES ($1, $2, $3, $4, $5, $6, now())`, id, userID, a.Kind, a.ProviderID, a.Subject, a.Label); err != nil {
		return err
	}
	return audit(ctx, tx, &userID, "identity.linked", map[string]any{"kind": a.Kind, "provider": a.ProviderID, "label": a.Label})
}

// createExternalUser adds a user without a local password, picking a free username.
func (s *Service) createExternalUser(ctx context.Context, tx pgx.Tx, a externalAccount) (User, error) {
	base := usernameFrom(a.Username)
	if base == "" && a.Email != "" {
		base = usernameFrom(strings.SplitN(a.Email, "@", 2)[0])
	}
	if base == "" {
		base = "user"
	}
	name := ""
	for i := 1; i <= 100; i++ {
		cand := base
		if i > 1 {
			suffix := "-" + strconv.Itoa(i)
			r := []rune(base)
			if len(r)+len(suffix) > 64 {
				r = r[:64-len(suffix)]
			}
			cand = string(r) + suffix
		}
		var taken bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM users WHERE lower(username) = lower($1))`, cand).Scan(&taken); err != nil {
			return User{}, err
		}
		if !taken {
			name = cand
			break
		}
	}
	if name == "" {
		return User{}, ErrUsernameTaken
	}
	isAdmin := a.IsAdmin != nil && *a.IsAdmin
	u, err := insertUser(ctx, tx, name, "", isAdmin, nil)
	if err != nil {
		return User{}, err
	}
	var email *string
	if a.Email != "" && a.EmailVerified {
		email = &a.Email
	}
	if _, err := tx.Exec(ctx, `UPDATE users SET display_name = NULLIF($2, ''), email = $3 WHERE id = $1`,
		u.ID, truncate(a.DisplayName, 100), email); err != nil {
		return User{}, err
	}
	if err := audit(ctx, tx, nil, "user.created", map[string]any{"user_id": u.ID, "username": u.Username, "is_admin": isAdmin,
		"source": a.Kind + ":" + a.ProviderID}); err != nil {
		return User{}, err
	}
	return u, nil
}

// usernameFrom turns a provider's name into a valid username (or "").
func usernameFrom(s string) string {
	var b strings.Builder
	for _, r := range strings.TrimSpace(s) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r), strings.ContainsRune("._@- ", r):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := strings.Trim(b.String(), " _")
	if r := []rune(out); len(r) > 64 {
		out = strings.TrimSpace(string(r[:64]))
	}
	if _, err := NormalizeUsername(out); err != nil {
		return ""
	}
	return out
}

// verifyStored checks a password against a stored hash. Accounts without a local
// password ("") never match, after the same work as a real check.
func verifyStored(password, hash string) (bool, error) {
	if hash == "" {
		_, _ = VerifyPassword(password, dummyHash)
		return false, nil
	}
	return VerifyPassword(password, hash)
}

// confirmPassword re-checks a signed-in user's password: the local one, or for a
// directory account the LDAP password.
func (s *Service) confirmPassword(ctx context.Context, userID uuid.UUID, password string) (bool, error) {
	var hash, username string
	var ldapLinked bool
	if err := s.pool.QueryRow(ctx, `SELECT password_hash, username,
			EXISTS (SELECT 1 FROM user_identities WHERE user_id = users.id AND kind = 'ldap')
		FROM users WHERE id = $1`, userID).Scan(&hash, &username, &ldapLinked); err != nil {
		return false, err
	}
	if hash == "" && ldapLinked {
		cfg, err := s.LDAPSettings(ctx)
		if err != nil {
			return false, err
		}
		if !cfg.Enabled {
			return false, nil
		}
		res, err := s.ldapAuthenticate(ctx, cfg, username, password)
		if errors.Is(err, ErrInvalidCredentials) {
			return false, nil
		}
		return err == nil && res != nil, err
	}
	return verifyStored(password, hash)
}
