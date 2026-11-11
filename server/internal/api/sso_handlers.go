package api

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
)

// OIDCFlowCookie binds a pending single sign-on to the browser that started it.
const OIDCFlowCookie = "streamero_oidc"

// oidcFlowCookie is SameSite=Lax: it must reach the callback, which is a top-level
// navigation coming from the identity provider's site.
func oidcFlowCookie(ctx context.Context, value string) string {
	c := &http.Cookie{ // #nosec G124 -- Secure follows the connection, like the session cookie
		Name: OIDCFlowCookie, Value: value, Path: "/api/v1/auth/oidc/", HttpOnly: true,
		Secure: isSecure(ctx), SameSite: http.SameSiteLaxMode, MaxAge: int((10 * time.Minute).Seconds()),
	}
	return c.String()
}

// ssoErrorCode is the code a failed browser sign-in is redirected with.
func ssoErrorCode(err error) string {
	switch {
	case errors.Is(err, auth.ErrInvalidState):
		return "invalid_state"
	case errors.Is(err, auth.ErrProviderFailed):
		return "provider_error"
	case errors.Is(err, auth.ErrNoAccount):
		return "no_account"
	case errors.Is(err, auth.ErrNotAllowed):
		return "not_allowed"
	case errors.Is(err, auth.ErrAccountDisabled), errors.Is(err, auth.ErrInvalidCredentials):
		return "account_disabled"
	case errors.Is(err, auth.ErrIdentityTaken):
		return "identity_taken"
	case errors.Is(err, auth.ErrAlreadyLinked):
		return "already_linked"
	case errors.Is(err, auth.ErrNotFound):
		return "unknown_provider"
	}
	var inv *auth.InvalidSettingError
	if errors.As(err, &inv) {
		return "not_configured"
	}
	return "internal"
}

func withQuery(base, key, value string) string {
	return base + "?" + url.Values{key: {value}}.Encode()
}

// GetAuthProviders implements GET /auth/providers.
func (h *Handler) GetAuthProviders(ctx context.Context) (*gen.AuthProviders, error) {
	password, ldap, buttons, err := h.Auth().LoginMethods(ctx)
	if err != nil {
		return nil, err
	}
	out := &gen.AuthProviders{PasswordLogin: password, Ldap: ldap, Oidc: []gen.OidcProviderButton{}}
	for _, b := range buttons {
		out.Oidc = append(out.Oidc, gen.OidcProviderButton{ID: b.ID, Label: b.Label, AuthorizeUrl: b.AuthorizeURL})
	}
	return out, nil
}

// OidcAuthorize implements GET /auth/oidc/{providerId}/authorize. Failures redirect
// back to the client (never a JSON error: this is a browser navigation).
func (h *Handler) OidcAuthorize(ctx context.Context, params gen.OidcAuthorizeParams) (*gen.OidcRedirect, error) {
	req := auth.AuthorizeRequest{ProviderID: params.ProviderId, Client: string(params.Client.Or(gen.OidcAuthorizeClientWeb))}
	if req.Client == "app" {
		req.AppRedirect, req.AppChallenge = params.RedirectUri.Value, params.CodeChallenge.Value
	}
	authURL, binding, err := h.Auth().OIDCAuthorize(ctx, req)
	if err != nil {
		slog.InfoContext(ctx, "oidc: sign-in not started", "provider", params.ProviderId, "err", err)
		if req.Client == "app" && req.AppRedirect == auth.AppRedirectURI {
			return &gen.OidcRedirect{Location: withQuery(auth.AppRedirectURI, "error", ssoErrorCode(err))}, nil
		}
		return &gen.OidcRedirect{Location: withQuery("/login", "sso_error", ssoErrorCode(err))}, nil
	}
	return &gen.OidcRedirect{Location: authURL, SetCookie: gen.NewOptString(oidcFlowCookie(ctx, binding))}, nil
}

// OidcCallback implements GET /auth/oidc/{providerId}/callback.
func (h *Handler) OidcCallback(ctx context.Context, params gen.OidcCallbackParams) (*gen.OidcRedirect, error) {
	dev := auth.Device{Name: "Web browser", Client: "Streamero Web (single sign-on)"}
	res, err := h.Auth().OIDCCallback(ctx, params.ProviderId, params.Code.Value, params.State.Value,
		params.StreameroOidc.Value, params.Error.Value, dev)
	if err != nil {
		code := ssoErrorCode(err)
		if code == "internal" {
			slog.ErrorContext(ctx, "oidc: callback failed", "provider", params.ProviderId, "err", err)
		} else {
			slog.InfoContext(ctx, "oidc: sign-in refused", "provider", params.ProviderId, "reason", code)
		}
		if params.Error.Value != "" {
			slog.InfoContext(ctx, "oidc: provider error", "provider", params.ProviderId, "error", params.Error.Value,
				"description", params.ErrorDescription.Value)
		}
		switch res.Client {
		case "app":
			return &gen.OidcRedirect{Location: withQuery(res.AppRedirect, "error", code)}, nil
		case "link":
			return &gen.OidcRedirect{Location: withQuery("/settings/account", "sso_error", code)}, nil
		}
		return &gen.OidcRedirect{Location: withQuery("/login", "sso_error", code)}, nil
	}
	switch res.Client {
	case "app":
		// A one-time code only the app holding the PKCE verifier can redeem; no session token in the URL.
		return &gen.OidcRedirect{Location: withQuery(res.AppRedirect, "code", res.HandoffCode)}, nil
	case "link":
		return &gen.OidcRedirect{Location: withQuery("/settings/account", "linked", params.ProviderId)}, nil
	}
	return &gen.OidcRedirect{Location: "/", SetCookie: gen.NewOptString(sessionCookie(ctx, res.Token, auth.SessionTTL))}, nil
}

// OidcExchange implements POST /auth/oidc/exchange.
func (h *Handler) OidcExchange(ctx context.Context, req *gen.OidcExchangeRequest) (*gen.SignedInHeaders, error) {
	p, token, err := h.Auth().OIDCExchange(ctx, req.Code, req.CodeVerifier, device(req.Device))
	if err != nil {
		return nil, err
	}
	return h.signedIn(ctx, p, token, false), nil
}

// OidcLink implements POST /auth/oidc/{providerId}/link.
func (h *Handler) OidcLink(ctx context.Context, params gen.OidcLinkParams) (*gen.OidcLinkStartHeaders, error) {
	p, err := adult(ctx)
	if err != nil {
		return nil, err
	}
	if p.SessionID == uuid.Nil {
		return nil, auth.ErrSessionRequired
	}
	authURL, binding, err := h.Auth().OIDCAuthorize(ctx, auth.AuthorizeRequest{ProviderID: params.ProviderId, Client: "link", LinkUser: &p.User.ID})
	if err != nil {
		return nil, err
	}
	return &gen.OidcLinkStartHeaders{SetCookie: gen.NewOptString(oidcFlowCookie(ctx, binding)),
		Response: gen.OidcLinkStart{AuthorizationUrl: authURL}}, nil
}

// ListIdentities implements GET /auth/identities.
func (h *Handler) ListIdentities(ctx context.Context) (*gen.LinkedIdentities, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	has, ids, err := h.Auth().Identities(ctx, p.User.ID)
	if err != nil {
		return nil, err
	}
	out := &gen.LinkedIdentities{HasPassword: has, Identities: []gen.LinkedIdentity{}}
	for _, x := range ids {
		li := gen.LinkedIdentity{ID: x.ID, Kind: gen.LinkedIdentityKind(x.Kind), ProviderId: x.ProviderID,
			ProviderLabel: x.ProviderLabel, Label: x.Label, CreatedAt: x.CreatedAt}
		if x.LastLoginAt != nil {
			li.LastLoginAt = gen.NewOptDateTime(*x.LastLoginAt)
		}
		out.Identities = append(out.Identities, li)
	}
	return out, nil
}

// UnlinkIdentity implements DELETE /auth/identities/{identityId}.
func (h *Handler) UnlinkIdentity(ctx context.Context, params gen.UnlinkIdentityParams) error {
	p, err := adult(ctx)
	if err != nil {
		return err
	}
	return h.Auth().UnlinkIdentity(ctx, p, params.IdentityId)
}

func (h *Handler) toAuthSettings(a auth.SignInSettings) *gen.AuthSettings {
	return &gen.AuthSettings{PublicUrl: a.PublicURL, DisablePasswordLogin: a.DisablePasswordLogin,
		RecoveryActive: h.Auth().PasswordRecovery}
}

// GetAuthSettings implements GET /settings/auth.
func (h *Handler) GetAuthSettings(ctx context.Context) (*gen.AuthSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	a, err := h.Auth().SignInSettings(ctx)
	if err != nil {
		return nil, err
	}
	return h.toAuthSettings(a), nil
}

// UpdateAuthSettings implements PUT /settings/auth.
func (h *Handler) UpdateAuthSettings(ctx context.Context, req *gen.AuthSettingsInput) (*gen.AuthSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	a, err := h.Auth().UpdateAuthSettings(ctx, p, auth.SignInSettings{PublicURL: req.PublicUrl, DisablePasswordLogin: req.DisablePasswordLogin})
	if err != nil {
		return nil, err
	}
	return h.toAuthSettings(a), nil
}

func toOIDCProvider(p auth.OIDCProvider, publicURL string) gen.OidcProvider {
	return gen.OidcProvider{ID: p.ID, Label: p.Label, Issuer: p.Issuer, ClientId: p.ClientID, ClientSecretSet: p.ClientSecret != "",
		Scopes: p.Scopes, UsernameClaim: p.UsernameClaim, EmailClaim: p.EmailClaim, NameClaim: p.NameClaim,
		GroupsClaim: p.GroupsClaim, AdminGroup: p.AdminGroup, AllowedGroup: p.AllowedGroup, AutoCreate: p.AutoCreate,
		LinkByEmail: p.LinkByEmail, TrustEmail: p.TrustEmail, Enabled: p.Enabled,
		RedirectUri: auth.OIDCRedirectURI(publicURL, p.ID)}
}

func fromOIDCInput(in *gen.OidcProviderInput) auth.OIDCInput {
	return auth.OIDCInput{OIDCProvider: auth.OIDCProvider{ID: in.ID, Label: in.Label, Issuer: in.Issuer, ClientID: in.ClientId,
		ClientSecret: in.ClientSecret.Value, Scopes: in.Scopes, UsernameClaim: in.UsernameClaim.Value,
		EmailClaim: in.EmailClaim.Value, NameClaim: in.NameClaim.Value, GroupsClaim: in.GroupsClaim.Value,
		AdminGroup: in.AdminGroup.Value, AllowedGroup: in.AllowedGroup.Value, AutoCreate: in.AutoCreate.Value,
		LinkByEmail: in.LinkByEmail.Or(true), TrustEmail: in.TrustEmail.Value, Enabled: in.Enabled.Or(true)},
		RemoveSecret: in.RemoveClientSecret.Value}
}

func (h *Handler) publicURL(ctx context.Context) string {
	a, err := h.Auth().SignInSettings(ctx)
	if err != nil {
		return ""
	}
	return a.PublicURL
}

// ListOidcProviders implements GET /settings/auth/oidc.
func (h *Handler) ListOidcProviders(ctx context.Context) ([]gen.OidcProvider, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	ps, err := h.Auth().OIDCProviders(ctx)
	if err != nil {
		return nil, err
	}
	pub := h.publicURL(ctx)
	out := make([]gen.OidcProvider, 0, len(ps))
	for _, p := range ps {
		out = append(out, toOIDCProvider(p, pub))
	}
	return out, nil
}

// CreateOidcProvider implements POST /settings/auth/oidc.
func (h *Handler) CreateOidcProvider(ctx context.Context, req *gen.OidcProviderInput) (*gen.OidcProvider, error) {
	a, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.Auth().CreateOIDCProvider(ctx, a, fromOIDCInput(req))
	if err != nil {
		return nil, err
	}
	out := toOIDCProvider(p, h.publicURL(ctx))
	return &out, nil
}

// UpdateOidcProvider implements PUT /settings/auth/oidc/{providerId}.
func (h *Handler) UpdateOidcProvider(ctx context.Context, req *gen.OidcProviderInput, params gen.UpdateOidcProviderParams) (*gen.OidcProvider, error) {
	a, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	p, err := h.Auth().UpdateOIDCProvider(ctx, a, params.ProviderId, fromOIDCInput(req))
	if err != nil {
		return nil, err
	}
	out := toOIDCProvider(p, h.publicURL(ctx))
	return &out, nil
}

// DeleteOidcProvider implements DELETE /settings/auth/oidc/{providerId}.
func (h *Handler) DeleteOidcProvider(ctx context.Context, params gen.DeleteOidcProviderParams) error {
	a, err := admin(ctx)
	if err != nil {
		return err
	}
	return h.Auth().DeleteOIDCProvider(ctx, a, params.ProviderId)
}

// TestOidcProvider implements POST /settings/auth/oidc-test.
func (h *Handler) TestOidcProvider(ctx context.Context, req *gen.OidcTestRequest) (*gen.OidcTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r, err := h.Auth().TestOIDC(ctx, req.Issuer)
	if err != nil {
		var inv *auth.InvalidSettingError
		if errors.As(err, &inv) {
			return nil, err
		}
		return nil, &StatusError{Status: http.StatusBadGateway, Code: "provider_unreachable", Message: err.Error()}
	}
	out := &gen.OidcTestResult{Issuer: r.Issuer, AuthorizationEndpoint: r.AuthURL, TokenEndpoint: r.TokenURL,
		SigningKeys: r.Keys, PkceS256: r.PKCES256, ScopesSupported: r.Scopes}
	if r.UserInfoURL != "" {
		out.UserinfoEndpoint = gen.NewOptString(r.UserInfoURL)
	}
	return out, nil
}

func fromLDAPInput(in gen.LdapSettingsInput) auth.LDAPInput {
	return auth.LDAPInput{LDAPSettings: auth.LDAPSettings{Enabled: in.Enabled, URL: in.URL, StartTLS: in.StartTls,
		SkipVerify: in.SkipVerify, CACert: in.CaCert.Value, BindDN: in.BindDn.Value, BindPassword: in.BindPassword.Value,
		BaseDN: in.BaseDn, UserFilter: in.UserFilter, UsernameAttr: in.UsernameAttribute.Value,
		EmailAttr: in.EmailAttribute.Value, NameAttr: in.NameAttribute.Value, GroupAttr: in.GroupAttribute.Value,
		GroupFilter: in.GroupFilter.Value, AdminGroup: in.AdminGroup.Value, AllowedGroup: in.AllowedGroup.Value},
		RemoveBindPassword: in.RemoveBindPassword.Value}
}

func toLDAPSettings(c auth.LDAPSettings) *gen.LdapSettings {
	return &gen.LdapSettings{Enabled: c.Enabled, URL: c.URL, StartTls: c.StartTLS, SkipVerify: c.SkipVerify, CaCert: c.CACert,
		BindDn: c.BindDN, BindPasswordSet: c.BindPassword != "", BaseDn: c.BaseDN, UserFilter: c.UserFilter,
		UsernameAttribute: c.UsernameAttr, EmailAttribute: c.EmailAttr, NameAttribute: c.NameAttr,
		GroupAttribute: c.GroupAttr, GroupFilter: c.GroupFilter, AdminGroup: c.AdminGroup, AllowedGroup: c.AllowedGroup}
}

// GetLdapSettings implements GET /settings/auth/ldap.
func (h *Handler) GetLdapSettings(ctx context.Context) (*gen.LdapSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	c, err := h.Auth().LDAPSettings(ctx)
	if err != nil {
		return nil, err
	}
	return toLDAPSettings(c), nil
}

// UpdateLdapSettings implements PUT /settings/auth/ldap.
func (h *Handler) UpdateLdapSettings(ctx context.Context, req *gen.LdapSettingsInput) (*gen.LdapSettings, error) {
	a, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	c, err := h.Auth().SaveLDAP(ctx, a, fromLDAPInput(*req))
	if err != nil {
		return nil, err
	}
	return toLDAPSettings(c), nil
}

// TestLdapSettings implements POST /settings/auth/ldap/test.
func (h *Handler) TestLdapSettings(ctx context.Context, req *gen.LdapTestRequest) (*gen.LdapTestResult, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	e, err := h.Auth().TestLDAP(ctx, fromLDAPInput(req.Settings), req.Username.Value)
	if err != nil {
		var inv *auth.InvalidSettingError
		if errors.As(err, &inv) {
			return nil, err
		}
		return &gen.LdapTestResult{Ok: false, Message: err.Error()}, nil
	}
	if e == nil {
		return &gen.LdapTestResult{Ok: true, Message: "connected and bound"}, nil
	}
	return &gen.LdapTestResult{Ok: true, Message: "user found", UserDn: gen.NewOptString(e.DN),
		Username: gen.NewOptString(e.Username), Email: gen.NewOptString(e.Email), DisplayName: gen.NewOptString(e.DisplayName),
		Groups: append([]string{}, e.Groups...), IsAdmin: gen.NewOptBool(e.IsAdmin), Allowed: gen.NewOptBool(e.Allowed)}, nil
}

// mapSSOError converts single sign-on and directory errors into API errors.
func mapSSOError(err error) *StatusError {
	var inv *auth.InvalidSettingError
	if errors.As(err, &inv) {
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_settings", Message: inv.Msg}
	}
	for _, m := range []struct {
		err    error
		status int
		code   string
	}{
		{auth.ErrPasswordLoginDisabled, http.StatusForbidden, "password_login_disabled"},
		{auth.ErrNoLocalPassword, http.StatusConflict, "no_local_password"},
		{auth.ErrNoAccount, http.StatusForbidden, "no_account"},
		{auth.ErrNotAllowed, http.StatusForbidden, "not_allowed"},
		{auth.ErrAccountDisabled, http.StatusForbidden, "account_disabled"},
		{auth.ErrIdentityTaken, http.StatusConflict, "identity_taken"},
		{auth.ErrAlreadyLinked, http.StatusConflict, "already_linked"},
		{auth.ErrLastSignInMethod, http.StatusConflict, "last_sign_in_method"},
		{auth.ErrNoSSOAdmin, http.StatusConflict, "no_sso_admin"},
		{auth.ErrInvalidState, http.StatusBadRequest, "invalid_state"},
		{auth.ErrProviderFailed, http.StatusBadGateway, "provider_error"},
	} {
		if errors.Is(err, m.err) {
			return &StatusError{Status: m.status, Code: m.code, Message: err.Error()}
		}
	}
	return nil
}
