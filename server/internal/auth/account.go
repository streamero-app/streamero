package auth

import (
	"context"
	"errors"
	"net/mail"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

// Account holds the details users change about themselves.
type Account struct {
	DisplayName string
	Email       string
	Language    string
	HasAvatar   bool
}

// AccountInput is a change of account details; nil fields stay as they are.
type AccountInput struct {
	DisplayName, Email, Language *string
}

// ErrInvalidEmail is returned for an e-mail address that cannot be parsed.
var ErrInvalidEmail = errors.New("invalid e-mail address")

// Account returns a user's own details.
func (s *Service) Account(ctx context.Context, userID uuid.UUID) (Account, error) {
	var a Account
	err := s.pool.QueryRow(ctx, `SELECT COALESCE(display_name, ''), COALESCE(email, ''), COALESCE(language, ''), has_avatar
		FROM users WHERE id = $1`, userID).Scan(&a.DisplayName, &a.Email, &a.Language, &a.HasAvatar)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// UpdateAccount changes the given fields of a user's own details.
func (s *Service) UpdateAccount(ctx context.Context, userID uuid.UUID, in AccountInput) (Account, error) {
	trim := func(p *string) *string {
		if p == nil {
			return nil
		}
		v := strings.TrimSpace(*p)
		return &v
	}
	in.DisplayName, in.Email, in.Language = trim(in.DisplayName), trim(in.Email), trim(in.Language)
	if in.Email != nil && *in.Email != "" {
		addr, err := mail.ParseAddress(*in.Email)
		if err != nil || addr.Address != *in.Email {
			return Account{}, ErrInvalidEmail
		}
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET
			display_name = CASE WHEN $2::text IS NULL THEN display_name ELSE NULLIF($2, '') END,
			email = CASE WHEN $3::text IS NULL THEN email ELSE NULLIF($3, '') END,
			language = CASE WHEN $4::text IS NULL THEN language ELSE NULLIF($4, '') END
		WHERE id = $1`, userID, in.DisplayName, in.Email, in.Language)
	if err != nil {
		return Account{}, err
	}
	if tag.RowsAffected() == 0 {
		return Account{}, ErrNotFound
	}
	_ = s.Audit(ctx, &userID, "account.updated", nil)
	return s.Account(ctx, userID)
}

// SetUserAvatar records whether a user has a picture.
func (s *Service) SetUserAvatar(ctx context.Context, userID uuid.UUID, has bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE users SET has_avatar = $2 WHERE id = $1`, userID, has)
	return err
}

// OwnsProfile reports whether a profile belongs to the user.
func (s *Service) OwnsProfile(ctx context.Context, userID, profileID uuid.UUID) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM profiles WHERE id = $1 AND user_id = $2)`, profileID, userID).Scan(&ok)
	return ok, err
}

// SetProfileAvatar records whether a profile has a picture.
func (s *Service) SetProfileAvatar(ctx context.Context, profileID uuid.UUID, has bool) error {
	_, err := s.pool.Exec(ctx, `UPDATE profiles SET has_avatar = $2 WHERE id = $1`, profileID, has)
	return err
}
