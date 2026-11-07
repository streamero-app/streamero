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

// adult returns the principal unless the caller is in a kids profile.
func adult(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if p.IsKid() {
		return p, auth.ErrKidProfile
	}
	return p, nil
}

func admin(ctx context.Context) (auth.Principal, error) {
	p, err := adult(ctx)
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
	if p.Profile != nil {
		u.CurrentProfile = gen.NewOptProfile(toProfile(*p.Profile))
	}
	return &u, nil
}

// ChangePassword implements POST /auth/password.
func (h *Handler) ChangePassword(ctx context.Context, req *gen.ChangePasswordRequest) error {
	p, err := adult(ctx)
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
	var perms *auth.Permissions
	if v, ok := req.Permissions.Get(); ok {
		pp := fromPermissions(v)
		perms = &pp
	}
	u, err := h.Auth().CreateUser(ctx, p, string(req.Username), string(req.Password), req.IsAdmin.Value, perms)
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
	if v, ok := req.Permissions.Get(); ok {
		pp := fromPermissions(v)
		upd.Permissions = &pp
	}
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
	p, err := adult(ctx)
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
	p, err := adult(ctx)
	if err != nil {
		return err
	}
	return h.Auth().RevokeSession(ctx, p, params.SessionId)
}

// InitiateQuickConnect implements POST /quickconnect/initiate.
func (h *Handler) InitiateQuickConnect(ctx context.Context, req gen.OptDeviceInfo) (*gen.QuickConnectStart, error) {
	st, err := h.Auth().InitiateQuickConnect(ctx, device(req))
	if err != nil {
		return nil, err
	}
	return &gen.QuickConnectStart{Code: st.Code, Secret: st.Secret, ExpiresAt: st.ExpiresAt}, nil
}

// PollQuickConnect implements POST /quickconnect/poll.
func (h *Handler) PollQuickConnect(ctx context.Context, req *gen.QuickConnectPollRequest) (gen.PollQuickConnectRes, error) {
	p, token, err := h.Auth().PollQuickConnect(ctx, req.Secret)
	if errors.Is(err, auth.ErrQuickConnectPending) {
		return &gen.PollQuickConnectAccepted{}, nil
	}
	if err != nil {
		return nil, err
	}
	return h.signedIn(ctx, p, token, req.UseCookie.Value), nil
}

// AuthorizeQuickConnect implements POST /quickconnect/authorize.
func (h *Handler) AuthorizeQuickConnect(ctx context.Context, req *gen.TOTPCodeRequest) (*gen.QuickConnectApproved, error) {
	p, err := adult(ctx)
	if err != nil {
		return nil, err
	}
	dev, err := h.Auth().AuthorizeQuickConnect(ctx, p, req.Code)
	if err != nil {
		return nil, err
	}
	return &gen.QuickConnectApproved{DeviceName: dev}, nil
}

// StartTOTPSetup implements POST /auth/totp/setup.
func (h *Handler) StartTOTPSetup(ctx context.Context) (*gen.TOTPSetup, error) {
	p, err := adult(ctx)
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
	p, err := adult(ctx)
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
	p, err := adult(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DisableTOTP(ctx, p, req.Password)
}

// RenameSession implements PATCH /sessions/{sessionId}.
func (h *Handler) RenameSession(ctx context.Context, req *gen.RenameSessionRequest, params gen.RenameSessionParams) error {
	p, err := adult(ctx)
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
	out := gen.User{ID: u.ID, Username: u.Username, IsAdmin: u.IsAdmin, Disabled: u.Disabled, TotpEnabled: u.TOTPEnabled,
		Permissions: toPermissions(u.Permissions), CreatedAt: u.CreatedAt}
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
	if le := mapLibraryError(err); le != nil {
		return le
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
	case errors.Is(err, auth.ErrPinRequired):
		return &StatusError{Status: http.StatusUnauthorized, Code: "pin_required", Message: err.Error()}
	case errors.Is(err, auth.ErrInvalidPin):
		return &StatusError{Status: http.StatusUnauthorized, Code: "invalid_pin", Message: err.Error()}
	case errors.Is(err, auth.ErrProfileLocked):
		return &StatusError{Status: http.StatusTooManyRequests, Code: "profile_locked", Message: err.Error()}
	case errors.Is(err, auth.ErrKidProfile):
		return &StatusError{Status: http.StatusForbidden, Code: "kid_profile", Message: err.Error()}
	case errors.Is(err, auth.ErrTooManyProfiles):
		return &StatusError{Status: http.StatusConflict, Code: "too_many_profiles", Message: err.Error()}
	case errors.Is(err, auth.ErrInvalidName):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_name", Message: err.Error()}
	case errors.Is(err, auth.ErrSessionRequired):
		return &StatusError{Status: http.StatusForbidden, Code: "session_required", Message: err.Error()}
	case errors.Is(err, auth.ErrSelf):
		return &StatusError{Status: http.StatusConflict, Code: "self", Message: err.Error()}
	}
	return nil
}

func toPermissions(p auth.Permissions) gen.UserPermissions {
	return gen.UserPermissions{
		AllLibraries: p.AllLibraries, Download: p.Download, VideoTranscoding: p.VideoTranscoding,
		AudioTranscoding: p.AudioTranscoding, Remux: p.Remux, LiveTv: p.LiveTV, ManageLiveTv: p.ManageLiveTV,
		SyncPlay: p.SyncPlay, DeleteMedia: p.DeleteMedia, ManageCollections: p.ManageCollections,
		EditSubtitles: p.EditSubtitles, RemoteControl: p.RemoteControl, MaxSessions: p.MaxSessions,
	}
}

func fromPermissions(p gen.UserPermissions) auth.Permissions {
	return auth.Permissions{
		AllLibraries: p.AllLibraries, Download: p.Download, VideoTranscoding: p.VideoTranscoding,
		AudioTranscoding: p.AudioTranscoding, Remux: p.Remux, LiveTV: p.LiveTv, ManageLiveTV: p.ManageLiveTv,
		SyncPlay: p.SyncPlay, DeleteMedia: p.DeleteMedia, ManageCollections: p.ManageCollections,
		EditSubtitles: p.EditSubtitles, RemoteControl: p.RemoteControl, MaxSessions: p.MaxSessions,
	}
}

// ListInvites implements GET /invites.
func (h *Handler) ListInvites(ctx context.Context) ([]gen.Invite, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	invites, err := h.Auth().ListInvites(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Invite, 0, len(invites))
	for _, i := range invites {
		out = append(out, toInvite(i))
	}
	return out, nil
}

// CreateInvite implements POST /invites.
func (h *Handler) CreateInvite(ctx context.Context, req *gen.CreateInviteRequest) (*gen.CreatedInvite, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	var perms *auth.Permissions
	if v, ok := req.Permissions.Get(); ok {
		pp := fromPermissions(v)
		perms = &pp
	}
	inv, token, err := h.Auth().CreateInvite(ctx, p, req.Note.Value, time.Duration(req.ExpiresInHours)*time.Hour, req.MaxUses, perms)
	if err != nil {
		return nil, err
	}
	return &gen.CreatedInvite{Token: token, Invite: toInvite(inv)}, nil
}

// DeleteInvite implements DELETE /invites/{inviteId}.
func (h *Handler) DeleteInvite(ctx context.Context, params gen.DeleteInviteParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DeleteInvite(ctx, p, params.InviteId)
}

// GetInvite implements GET /invite/{token}.
func (h *Handler) GetInvite(ctx context.Context, params gen.GetInviteParams) (*gen.InviteInfo, error) {
	exp, err := h.Auth().CheckInvite(ctx, params.Token)
	if err != nil {
		return nil, err
	}
	return &gen.InviteInfo{ServerName: h.serverName(ctx), ExpiresAt: exp}, nil
}

// AcceptInvite implements POST /invite/{token}.
func (h *Handler) AcceptInvite(ctx context.Context, req *gen.AcceptInviteRequest, params gen.AcceptInviteParams) (*gen.SignedInHeaders, error) {
	p, token, err := h.Auth().AcceptInvite(ctx, params.Token, string(req.Username), string(req.Password), device(req.Device))
	if err != nil {
		return nil, err
	}
	return h.signedIn(ctx, p, token, req.UseCookie.Value), nil
}

func toInvite(i auth.Invite) gen.Invite {
	return gen.Invite{ID: i.ID, Note: i.Note, CreatedBy: i.CreatedBy, CreatedAt: i.CreatedAt, ExpiresAt: i.ExpiresAt,
		MaxUses: i.MaxUses, Uses: i.Uses, Permissions: toPermissions(i.Permissions)}
}

// ListProfiles implements GET /profiles.
func (h *Handler) ListProfiles(ctx context.Context) ([]gen.Profile, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	profiles, err := h.Auth().ListProfiles(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Profile, 0, len(profiles))
	for _, pr := range profiles {
		out = append(out, toProfile(pr))
	}
	return out, nil
}

// CreateProfile implements POST /profiles.
func (h *Handler) CreateProfile(ctx context.Context, req *gen.ProfileInput) (*gen.Profile, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	pr, err := h.Auth().CreateProfile(ctx, p, fromProfileInput(req))
	if err != nil {
		return nil, err
	}
	out := toProfile(pr)
	return &out, nil
}

// UpdateProfile implements PUT /profiles/{profileId}.
func (h *Handler) UpdateProfile(ctx context.Context, req *gen.ProfileInput, params gen.UpdateProfileParams) (*gen.Profile, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	pr, err := h.Auth().UpdateProfile(ctx, p, params.ProfileId, fromProfileInput(req))
	if err != nil {
		return nil, err
	}
	out := toProfile(pr)
	return &out, nil
}

// DeleteProfile implements DELETE /profiles/{profileId}.
func (h *Handler) DeleteProfile(ctx context.Context, params gen.DeleteProfileParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DeleteProfile(ctx, p, params.ProfileId)
}

// SelectProfile implements POST /profiles/{profileId}/select.
func (h *Handler) SelectProfile(ctx context.Context, req gen.OptPinRequest, params gen.SelectProfileParams) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().SelectProfile(ctx, p, params.ProfileId, req.Value.Pin.Value)
}

// SelectMainProfile implements POST /profiles/main/select.
func (h *Handler) SelectMainProfile(ctx context.Context, req gen.OptPasswordConfirmRequest) error {
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return h.Auth().SelectMainProfile(ctx, p, req.Value.Password)
}

func toProfile(pr auth.Profile) gen.Profile {
	out := gen.Profile{ID: pr.ID, Name: pr.Name, Color: pr.Color, HasPin: pr.HasPin, IsKid: pr.IsKid}
	if pr.MaxRating != nil {
		out.MaxRating = gen.NewOptInt(*pr.MaxRating)
	}
	return out
}

func fromProfileInput(req *gen.ProfileInput) auth.ProfileInput {
	in := auth.ProfileInput{Name: req.Name, Color: req.Color, IsKid: req.IsKid, RemovePin: req.RemovePin.Value}
	if v, ok := req.MaxRating.Get(); ok {
		in.MaxRating = &v
	}
	if v, ok := req.Pin.Get(); ok {
		in.Pin = &v
	}
	return in
}
