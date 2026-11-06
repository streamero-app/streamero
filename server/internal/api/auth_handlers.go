package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/ogen-go/ogen/ogenerrors"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// SessionCookie is the HttpOnly cookie used by the web client.
const SessionCookie = "streamero_session"

type ctxKey int

const (
	principalKey ctxKey = iota
	secureKey
)

// WithSecure records whether the request reached the server over HTTPS
// (directly or via a reverse proxy) so that cookies get the Secure flag.
func WithSecure(ctx context.Context, secure bool) context.Context {
	return context.WithValue(ctx, secureKey, secure)
}

func isSecure(ctx context.Context) bool { v, _ := ctx.Value(secureKey).(bool); return v }

func principal(ctx context.Context) (auth.Principal, error) {
	p, ok := ctx.Value(principalKey).(auth.Principal)
	if !ok {
		return auth.Principal{}, auth.ErrUnauthenticated
	}
	return p, nil
}

func admin(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if !p.User.IsAdmin {
		return p, errForbidden
	}
	return p, nil
}

var errForbidden = &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "administrator rights required"}

// Security implements gen.SecurityHandler.
type Security struct{ Auth func() *auth.Service }

// HandleSessionToken resolves the bearer token (or the session cookie, which the
// router turns into a bearer token) into a principal.
func (s Security) HandleSessionToken(ctx context.Context, _ gen.OperationName, t gen.SessionToken) (context.Context, error) {
	svc := s.Auth()
	if svc == nil {
		return ctx, auth.ErrUnauthenticated
	}
	p, err := svc.Authenticate(ctx, t.Token)
	if err != nil {
		return ctx, err
	}
	return context.WithValue(ctx, principalKey, p), nil
}

func sessionCookie(ctx context.Context, token string, maxAge time.Duration) string {
	// Secure is set whenever the client connection is HTTPS (directly or via the reverse
	// proxy). It must stay off for plain-HTTP LAN access (http://host:8787) or the browser
	// would drop the cookie; HttpOnly and SameSite=Strict are always on.
	c := &http.Cookie{ // #nosec G124 -- Secure is conditional by design, see above
		Name:     SessionCookie,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   isSecure(ctx),
		SameSite: http.SameSiteStrictMode,
		MaxAge:   int(maxAge.Seconds()),
	}
	if maxAge < 0 {
		c.MaxAge = -1
	}
	return c.String()
}

func device(d gen.OptDeviceInfo) auth.Device {
	return auth.Device{Name: d.Value.Name.Value, Client: d.Value.Client.Value}
}

func (h *Handler) signedIn(ctx context.Context, p auth.Principal, token string, useCookie bool) *gen.SignedInHeaders {
	res := &gen.SignedInHeaders{Response: gen.AuthResult{SessionId: p.SessionID, User: toUser(p.User)}}
	if useCookie {
		res.SetCookie = gen.NewOptString(sessionCookie(ctx, token, auth.SessionTTL))
	} else {
		res.Response.Token = gen.NewOptString(token)
	}
	return res
}

// CompleteSetup implements POST /setup.
func (h *Handler) CompleteSetup(ctx context.Context, req *gen.SetupRequest) (*gen.SignedInHeaders, error) {
	p, token, err := h.Auth().Setup(ctx, req.SetupCode, req.ServerName.Value, string(req.Username), string(req.Password), device(req.Device))
	if err != nil {
		return nil, err
	}
	return h.signedIn(ctx, p, token, req.UseCookie.Value), nil
}

// Login implements POST /auth/login.
func (h *Handler) Login(ctx context.Context, req *gen.LoginRequest) (*gen.SignedInHeaders, error) {
	p, token, err := h.Auth().Login(ctx, req.Username, req.Password, req.TotpCode.Value, device(req.Device))
	if err != nil {
		return nil, err
	}
	return h.signedIn(ctx, p, token, req.UseCookie.Value), nil
}

// Logout implements POST /auth/logout.
func (h *Handler) Logout(ctx context.Context) (*gen.LogoutNoContent, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if err := h.Auth().Logout(ctx, p); err != nil {
		return nil, err
	}
	return &gen.LogoutNoContent{SetCookie: gen.NewOptString(sessionCookie(ctx, "", -1))}, nil
}

// GetMe implements GET /auth/me.
func (h *Handler) GetMe(ctx context.Context) (*gen.User, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	u := toUser(p.User)
	return &u, nil
}

// ChangePassword implements POST /auth/password.
func (h *Handler) ChangePassword(ctx context.Context, req *gen.ChangePasswordRequest) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().ChangePassword(ctx, p, req.CurrentPassword, string(req.NewPassword))
}

// ListUsers implements GET /users.
func (h *Handler) ListUsers(ctx context.Context) ([]gen.User, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	users, err := h.Auth().ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.User, 0, len(users))
	for _, u := range users {
		out = append(out, toUser(u))
	}
	return out, nil
}

// CreateUser implements POST /users.
func (h *Handler) CreateUser(ctx context.Context, req *gen.CreateUserRequest) (*gen.User, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	u, err := h.Auth().CreateUser(ctx, p, string(req.Username), string(req.Password), req.IsAdmin.Value)
	if err != nil {
		return nil, err
	}
	out := toUser(u)
	return &out, nil
}

// UpdateUser implements PATCH /users/{userId}.
func (h *Handler) UpdateUser(ctx context.Context, req *gen.UpdateUserRequest, params gen.UpdateUserParams) (*gen.User, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	var upd auth.UserUpdate
	if v, ok := req.IsAdmin.Get(); ok {
		upd.IsAdmin = &v
	}
	if v, ok := req.Disabled.Get(); ok {
		upd.Disabled = &v
	}
	if v, ok := req.Password.Get(); ok {
		pw := string(v)
		upd.Password = &pw
	}
	upd.ResetTwoFactor = req.ResetTwoFactor.Value
	u, err := h.Auth().UpdateUser(ctx, p, params.UserId, upd)
	if err != nil {
		return nil, err
	}
	out := toUser(u)
	return &out, nil
}

// DeleteUser implements DELETE /users/{userId}.
func (h *Handler) DeleteUser(ctx context.Context, params gen.DeleteUserParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DeleteUser(ctx, p, params.UserId)
}

// ListSessions implements GET /sessions.
func (h *Handler) ListSessions(ctx context.Context) ([]gen.Session, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	var only *uuid.UUID
	if !p.User.IsAdmin {
		only = &p.User.ID
	}
	sessions, err := h.Auth().ListSessions(ctx, only)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Session, 0, len(sessions))
	for _, s := range sessions {
		out = append(out, gen.Session{
			ID: s.ID, UserId: s.UserID, Username: s.Username, DeviceName: s.DeviceName, Client: s.Client,
			CreatedAt: s.CreatedAt, LastSeenAt: s.LastSeenAt, Current: s.ID == p.SessionID,
		})
	}
	return out, nil
}

// RevokeSession implements DELETE /sessions/{sessionId}.
func (h *Handler) RevokeSession(ctx context.Context, params gen.RevokeSessionParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().RevokeSession(ctx, p, params.SessionId)
}

// StartTOTPSetup implements POST /auth/totp/setup.
func (h *Handler) StartTOTPSetup(ctx context.Context) (*gen.TOTPSetup, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	st, err := h.Auth().StartTOTPSetup(ctx, p, h.serverName(ctx))
	if err != nil {
		return nil, err
	}
	return &gen.TOTPSetup{Secret: st.Secret, OtpauthUri: st.OtpauthURI, QrPng: st.QRPNG}, nil
}

// EnableTOTP implements POST /auth/totp/enable.
func (h *Handler) EnableTOTP(ctx context.Context, req *gen.TOTPCodeRequest) (*gen.RecoveryCodes, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	codes, err := h.Auth().EnableTOTP(ctx, p, req.Code)
	if err != nil {
		return nil, err
	}
	return &gen.RecoveryCodes{RecoveryCodes: codes}, nil
}

// DisableTOTP implements POST /auth/totp/disable.
func (h *Handler) DisableTOTP(ctx context.Context, req *gen.PasswordConfirmRequest) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DisableTOTP(ctx, p, req.Password)
}

// RenameSession implements PATCH /sessions/{sessionId}.
func (h *Handler) RenameSession(ctx context.Context, req *gen.RenameSessionRequest, params gen.RenameSessionParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().RenameSession(ctx, p, params.SessionId, req.DeviceName)
}

// ListAPIKeys implements GET /apikeys.
func (h *Handler) ListAPIKeys(ctx context.Context) ([]gen.ApiKey, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	keys, err := h.Auth().ListAPIKeys(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.ApiKey, 0, len(keys))
	for _, k := range keys {
		out = append(out, toAPIKey(k))
	}
	return out, nil
}

// CreateAPIKey implements POST /apikeys.
func (h *Handler) CreateAPIKey(ctx context.Context, req *gen.CreateApiKeyRequest) (*gen.CreatedApiKey, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	k, secret, err := h.Auth().CreateAPIKey(ctx, p, req.Name)
	if err != nil {
		return nil, err
	}
	return &gen.CreatedApiKey{Key: secret, ApiKey: toAPIKey(k)}, nil
}

// DeleteAPIKey implements DELETE /apikeys/{apiKeyId}.
func (h *Handler) DeleteAPIKey(ctx context.Context, params gen.DeleteAPIKeyParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DeleteAPIKey(ctx, p, params.ApiKeyId)
}

func toAPIKey(k auth.APIKey) gen.ApiKey {
	out := gen.ApiKey{ID: k.ID, Name: k.Name, Prefix: k.Prefix, CreatedBy: k.CreatedBy, CreatedAt: k.CreatedAt}
	if k.LastUsedAt != nil {
		out.LastUsedAt = gen.NewOptDateTime(*k.LastUsedAt)
	}
	return out
}

// GetServerSettings implements GET /settings/server.
func (h *Handler) GetServerSettings(ctx context.Context) (*gen.ServerSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return &gen.ServerSettings{ServerName: h.serverName(ctx)}, nil
}

// UpdateServerSettings implements PUT /settings/server.
func (h *Handler) UpdateServerSettings(ctx context.Context, req *gen.ServerSettings) (*gen.ServerSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	name := strings.TrimSpace(req.ServerName)
	if name == "" {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "server name is required"}
	}
	if err := h.Settings().Set(ctx, settings.KeyServerName, name); err != nil {
		return nil, err
	}
	return &gen.ServerSettings{ServerName: name}, nil
}

// serverName returns the admin-set name, falling back to the configured default.
func (h *Handler) serverName(ctx context.Context) string {
	if h.Settings == nil {
		return h.ServerName
	}
	if st := h.Settings(); st != nil {
		var name string
		if ok, err := st.Get(ctx, settings.KeyServerName, &name); err == nil && ok && name != "" {
			return name
		}
	}
	return h.ServerName
}

func toUser(u auth.User) gen.User {
	out := gen.User{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, Disabled: u.Disabled, TotpEnabled: u.TOTPEnabled, CreatedAt: u.CreatedAt}
	if u.LastLoginAt != nil {
		out.LastLoginAt = gen.NewOptDateTime(*u.LastLoginAt)
	}
	return out
}

// mapError converts service errors into API errors.
func mapError(err error) *StatusError {
	var se *StatusError
	if errors.As(err, &se) {
		return se
	}
	var locked *auth.LockedError
	var secErr *ogenerrors.SecurityError
	var decReq *ogenerrors.DecodeRequestError
	var decParams *ogenerrors.DecodeParamsError
	switch {
	case errors.As(err, &secErr):
		return &StatusError{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: "not signed in"}
	case errors.As(err, &decReq), errors.As(err, &decParams):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	case errors.As(err, &locked):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "account_locked", Message: err.Error()}
	case errors.Is(err, auth.ErrInvalidCredentials):
		return &StatusError{Status: http.StatusUnauthorized, Code: "invalid_credentials", Message: err.Error()}
	case errors.Is(err, auth.ErrUnauthenticated):
		return &StatusError{Status: http.StatusUnauthorized, Code: "unauthenticated", Message: err.Error()}
	case errors.Is(err, auth.ErrSetupDone):
		return &StatusError{Status: http.StatusConflict, Code: "setup_completed", Message: err.Error()}
	case errors.Is(err, auth.ErrBadSetupCode):
		return &StatusError{Status: http.StatusForbidden, Code: "invalid_setup_code", Message: err.Error()}
	case errors.Is(err, auth.ErrSetupThrottled):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "setup_throttled", Message: err.Error()}
	case errors.Is(err, auth.ErrUsernameTaken):
		return &StatusError{Status: http.StatusConflict, Code: "username_taken", Message: err.Error()}
	case errors.Is(err, auth.ErrInvalidUsername):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_username", Message: err.Error()}
	case errors.Is(err, auth.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, auth.ErrLastAdmin):
		return &StatusError{Status: http.StatusConflict, Code: "last_admin", Message: err.Error()}
	case errors.Is(err, auth.ErrTOTPRequired):
		return &StatusError{Status: http.StatusUnauthorized, Code: "totp_required", Message: err.Error()}
	case errors.Is(err, auth.ErrInvalidTOTP):
		return &StatusError{Status: http.StatusUnauthorized, Code: "invalid_totp", Message: err.Error()}
	case errors.Is(err, auth.ErrTOTPAlreadyOn):
		return &StatusError{Status: http.StatusConflict, Code: "totp_enabled", Message: err.Error()}
	case errors.Is(err, auth.ErrTOTPNotStarted):
		return &StatusError{Status: http.StatusConflict, Code: "totp_not_started", Message: err.Error()}
	case errors.Is(err, auth.ErrInvalidName):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_name", Message: err.Error()}
	case errors.Is(err, auth.ErrSessionRequired):
		return &StatusError{Status: http.StatusForbidden, Code: "session_required", Message: err.Error()}
	case errors.Is(err, auth.ErrSelf):
		return &StatusError{Status: http.StatusConflict, Code: "self", Message: err.Error()}
	}
	return nil
}
