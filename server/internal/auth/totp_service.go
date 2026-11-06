package auth

import (
	"context"
	"encoding/base64"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"rsc.io/qr"
)

const recoveryCodeCount = 10

// TOTPSetup is what the user adds to an authenticator app.
type TOTPSetup struct {
	Secret     string // base32
	OtpauthURI string
	QRPNG      string // base64 PNG of OtpauthURI
}

// StartTOTPSetup creates a new pending secret (replacing an unfinished one).
func (s *Service) StartTOTPSetup(ctx context.Context, p Principal, issuer string) (TOTPSetup, error) {
	if p.SessionID == uuid.Nil {
		return TOTPSetup{}, ErrSessionRequired
	}
	secret, err := newTOTPSecret()
	if err != nil {
		return TOTPSetup{}, err
	}
	tag, err := s.pool.Exec(ctx, `INSERT INTO user_totp (user_id, secret) VALUES ($1, $2)
		ON CONFLICT (user_id) DO UPDATE SET secret = EXCLUDED.secret, last_step = 0, created_at = now()
		WHERE NOT user_totp.enabled`, p.User.ID, secret)
	if err != nil {
		return TOTPSetup{}, err
	}
	if tag.RowsAffected() == 0 {
		return TOTPSetup{}, ErrTOTPAlreadyOn
	}
	uri := totpURI(issuer, p.User.Username, secret)
	code, err := qr.Encode(uri, qr.M)
	if err != nil {
		return TOTPSetup{}, err
	}
	return TOTPSetup{Secret: b32.EncodeToString(secret), OtpauthURI: uri, QRPNG: base64.StdEncoding.EncodeToString(code.PNG())}, nil
}

// EnableTOTP verifies a code for the pending secret, turns 2FA on and returns
// one-time recovery codes. Other sessions are signed out.
func (s *Service) EnableTOTP(ctx context.Context, p Principal, code string) ([]string, error) {
	if p.SessionID == uuid.Nil {
		return nil, ErrSessionRequired
	}
	codes, err := newRecoveryCodes(recoveryCodeCount)
	if err != nil {
		return nil, err
	}
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var secret []byte
		var enabled bool
		err := tx.QueryRow(ctx, `SELECT secret, enabled FROM user_totp WHERE user_id = $1 FOR UPDATE`, p.User.ID).Scan(&secret, &enabled)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrTOTPNotStarted
		}
		if err != nil {
			return err
		}
		if enabled {
			return ErrTOTPAlreadyOn
		}
		step, ok := verifyTOTP(secret, code, s.now(), 0)
		if !ok {
			return ErrInvalidTOTP
		}
		if _, err := tx.Exec(ctx, `UPDATE user_totp SET enabled = true, last_step = $2 WHERE user_id = $1`, p.User.ID, step); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, p.User.ID); err != nil {
			return err
		}
		for _, c := range codes {
			if _, err := tx.Exec(ctx, `INSERT INTO recovery_codes (user_id, code_hash) VALUES ($1, $2)`,
				p.User.ID, hashToken(normalizeRecoveryCode(c))); err != nil {
				return err
			}
		}
		if _, err := tx.Exec(ctx, `DELETE FROM sessions WHERE user_id = $1 AND id <> $2`, p.User.ID, p.SessionID); err != nil {
			return err
		}
		return audit(ctx, tx, &p.User.ID, "totp.enabled", nil)
	})
	if err != nil {
		return nil, err
	}
	return codes, nil
}

// DisableTOTP turns 2FA off after re-checking the password.
func (s *Service) DisableTOTP(ctx context.Context, p Principal, password string) error {
	if p.SessionID == uuid.Nil {
		return ErrSessionRequired
	}
	var hash string
	if err := s.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, p.User.ID).Scan(&hash); err != nil {
		return err
	}
	ok, err := VerifyPassword(password, hash)
	if err != nil {
		return err
	}
	if !ok {
		return ErrInvalidCredentials
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_totp WHERE user_id = $1`, p.User.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM recovery_codes WHERE user_id = $1`, p.User.ID); err != nil {
			return err
		}
		return audit(ctx, tx, &p.User.ID, "totp.disabled", nil)
	})
}

// checkSecondFactor accepts a current TOTP code (each step once) or an unused recovery code.
func (s *Service) checkSecondFactor(ctx context.Context, userID uuid.UUID, code string, now time.Time) (bool, error) {
	var secret []byte
	var lastStep int64
	if err := s.pool.QueryRow(ctx, `SELECT secret, last_step FROM user_totp WHERE user_id = $1 AND enabled`, userID).
		Scan(&secret, &lastStep); err != nil {
		return false, err
	}
	if step, ok := verifyTOTP(secret, code, now, lastStep); ok {
		// Conditional update: a concurrent login with the same code loses.
		tag, err := s.pool.Exec(ctx, `UPDATE user_totp SET last_step = $2 WHERE user_id = $1 AND last_step < $2`, userID, step)
		return err == nil && tag.RowsAffected() == 1, err
	}
	// used_at IS NULL is repeated on the outer row so a concurrent use of the same code loses.
	tag, err := s.pool.Exec(ctx, `UPDATE recovery_codes SET used_at = now()
		WHERE used_at IS NULL
		  AND id = (SELECT id FROM recovery_codes WHERE user_id = $1 AND code_hash = $2 AND used_at IS NULL LIMIT 1)`,
		userID, hashToken(normalizeRecoveryCode(code)))
	if err != nil {
		return false, err
	}
	if tag.RowsAffected() == 1 {
		_ = audit(ctx, s.pool, &userID, "totp.recovery_code_used", nil)
		return true, nil
	}
	return false, nil
}
