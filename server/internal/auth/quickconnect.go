package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// QuickConnectTTL is how long a code can be approved.
const QuickConnectTTL = 5 * time.Minute

// ErrQuickConnectPending means the code has not been approved yet.
var ErrQuickConnectPending = errors.New("quick connect code not approved yet")

// QuickConnectStart is returned to the new device.
type QuickConnectStart struct {
	Code      string
	Secret    string
	ExpiresAt time.Time
}

// InitiateQuickConnect creates a pending request with a unique 6-digit code.
func (s *Service) InitiateQuickConnect(ctx context.Context, dev Device) (QuickConnectStart, error) {
	now := s.now()
	if _, err := s.pool.Exec(ctx, `DELETE FROM quick_connect WHERE expires_at <= $1`, now); err != nil {
		return QuickConnectStart{}, err
	}
	secret, secretHash, err := newToken()
	if err != nil {
		return QuickConnectStart{}, err
	}
	id, err := uuid.NewV7()
	if err != nil {
		return QuickConnectStart{}, err
	}
	exp := now.Add(QuickConnectTTL)
	for range 5 { // retry on the rare code collision
		n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
		if err != nil {
			return QuickConnectStart{}, err
		}
		code := fmt.Sprintf("%06d", n.Int64())
		_, err = s.pool.Exec(ctx, `INSERT INTO quick_connect (id, secret_hash, code, device_name, client, created_at, expires_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7)`, id, secretHash, code, truncate(dev.Name, 100), truncate(dev.Client, 100), now, exp)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			continue
		}
		if err != nil {
			return QuickConnectStart{}, err
		}
		return QuickConnectStart{Code: code, Secret: secret, ExpiresAt: exp}, nil
	}
	return QuickConnectStart{}, errors.New("could not allocate a quick connect code")
}

// AuthorizeQuickConnect approves a code for the calling user and returns the device name.
func (s *Service) AuthorizeQuickConnect(ctx context.Context, p Principal, code string) (string, error) {
	code = strings.ReplaceAll(strings.TrimSpace(code), " ", "")
	var device string
	err := s.pool.QueryRow(ctx, `UPDATE quick_connect SET user_id = $2
		WHERE code = $1 AND expires_at > $3 AND user_id IS NULL RETURNING device_name`, code, p.User.ID, s.now()).Scan(&device)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	if err != nil {
		return "", err
	}
	_ = audit(ctx, s.pool, &p.User.ID, "quickconnect.authorized", map[string]any{"device": device})
	return device, nil
}

// PollQuickConnect signs the new device in once its code was approved.
// It returns ErrQuickConnectPending until then and ErrNotFound when expired or unknown.
func (s *Service) PollQuickConnect(ctx context.Context, secret string) (Principal, string, error) {
	var p Principal
	var token string
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var id uuid.UUID
		var userID *uuid.UUID
		var dev Device
		err := tx.QueryRow(ctx, `SELECT id, user_id, device_name, client FROM quick_connect
			WHERE secret_hash = $1 AND expires_at > $2 FOR UPDATE`, hashToken(secret), s.now()).
			Scan(&id, &userID, &dev.Name, &dev.Client)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if userID == nil {
			return ErrQuickConnectPending
		}
		u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 AND NOT disabled`, *userID))
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM quick_connect WHERE id = $1`, id); err != nil {
			return err
		}
		if dev.Client == "" {
			dev.Client = "Quick Connect"
		}
		p, token, err = createSession(ctx, tx, u, dev, s.now())
		if err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "login.succeeded", map[string]any{"session_id": p.SessionID, "device": dev.Name, "via": "quickconnect"})
	})
	return p, token, err
}

// QuickConnectState describes a pending request as the new device sees it.
type QuickConnectState struct {
	Code       string
	DeviceName string
	Client     string
	CreatedAt  time.Time
	Approved   bool
}

// QuickConnectStatus reports whether a request was approved without consuming it
// (PollQuickConnect signs the device in). ErrNotFound when expired or unknown.
func (s *Service) QuickConnectStatus(ctx context.Context, secret string) (QuickConnectState, error) {
	var st QuickConnectState
	err := s.pool.QueryRow(ctx, `SELECT code, device_name, client, created_at, user_id IS NOT NULL FROM quick_connect
		WHERE secret_hash = $1 AND expires_at > $2`, hashToken(secret), s.now()).
		Scan(&st.Code, &st.DeviceName, &st.Client, &st.CreatedAt, &st.Approved)
	if errors.Is(err, pgx.ErrNoRows) {
		return st, ErrNotFound
	}
	return st, err
}
