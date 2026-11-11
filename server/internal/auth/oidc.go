package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/oauth2"
)

// AppRedirectURI is the only redirect URI apps may ask for (custom URL scheme of the
// iOS and Android apps, D-037). The provider never sees it: it redirects to the
// server, which hands the app a one-time code bound to the app's PKCE challenge.
const AppRedirectURI = "streamero://oidc"

// Lifetimes of pending sign-ins and of app hand-off codes.
const (
	oidcFlowTTL      = 10 * time.Minute
	oidcHandoffTTL   = 2 * time.Minute
	oidcDiscoveryTTL = time.Hour
)

// OIDCProvider is a configured OpenID Connect provider.
type OIDCProvider struct {
	ID, Label, Issuer, ClientID, ClientSecret string
	Scopes                                    []string
	UsernameClaim, EmailClaim, NameClaim      string
	GroupsClaim, AdminGroup, AllowedGroup     string
	AutoCreate, LinkByEmail, TrustEmail       bool
	Enabled                                   bool
}

// OIDCInput is a provider change; an empty secret keeps the stored one.
type OIDCInput struct {
	OIDCProvider
	RemoveSecret bool
}

const oidcCols = `id, label, issuer, client_id, client_secret, scopes, username_claim, email_claim, name_claim, groups_claim,
	admin_group, allowed_group, auto_create, link_by_email, trust_email, enabled`

func scanOIDC(row pgx.Row) (OIDCProvider, error) {
	var p OIDCProvider
	err := row.Scan(&p.ID, &p.Label, &p.Issuer, &p.ClientID, &p.ClientSecret, &p.Scopes, &p.UsernameClaim, &p.EmailClaim,
		&p.NameClaim, &p.GroupsClaim, &p.AdminGroup, &p.AllowedGroup, &p.AutoCreate, &p.LinkByEmail, &p.TrustEmail, &p.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return p, ErrNotFound
	}
	return p, err
}

// OIDCProviders lists all providers.
func (s *Service) OIDCProviders(ctx context.Context) ([]OIDCProvider, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+oidcCols+` FROM oidc_providers ORDER BY lower(label), id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []OIDCProvider
	for rows.Next() {
		p, err := scanOIDC(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// OIDCRedirectURI is the redirect URI to register at the provider ("" without a public address).
func OIDCRedirectURI(publicURL, providerID string) string {
	if publicURL == "" {
		return ""
	}
	return publicURL + "/api/v1/auth/oidc/" + providerID + "/callback"
}

var (
	providerIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,31}$`)
	scopeRe      = regexp.MustCompile(`^[\x21\x23-\x5B\x5D-\x7E]{1,100}$`) // RFC 6749 scope-token
	claimRe      = regexp.MustCompile(`^[A-Za-z0-9_:./\-]{1,100}$`)
)

// ValidateIssuer accepts https issuers, and http only on loopback (development).
func ValidateIssuer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		return "", invalid("the issuer must be a URL such as https://auth.example.com/realms/home")
	}
	switch u.Scheme {
	case "https":
	case "http":
		host := u.Hostname()
		if ip := net.ParseIP(host); host != "localhost" && (ip == nil || !ip.IsLoopback()) {
			return "", invalid("the issuer must use https")
		}
	default:
		return "", invalid("the issuer must use https")
	}
	return raw, nil
}

func (s *Service) normalizeOIDC(ctx context.Context, in OIDCInput, existing *OIDCProvider) (OIDCProvider, error) {
	p := in.OIDCProvider
	p.ID = strings.TrimSpace(p.ID)
	if !providerIDRe.MatchString(p.ID) {
		return p, invalid("the id may use lower-case letters, digits and dashes (up to 32)")
	}
	p.Label = strings.TrimSpace(p.Label)
	if p.Label == "" {
		return p, invalid("a button label is required")
	}
	var err error
	if p.Issuer, err = ValidateIssuer(p.Issuer); err != nil {
		return p, err
	}
	p.ClientID = strings.TrimSpace(p.ClientID)
	if p.ClientID == "" {
		return p, invalid("a client ID is required")
	}
	if p.ClientSecret == "" && existing != nil && !in.RemoveSecret {
		p.ClientSecret = existing.ClientSecret
	}
	scopes := []string{"openid"}
	for _, sc := range p.Scopes {
		for _, f := range strings.Fields(sc) {
			if !scopeRe.MatchString(f) {
				return p, invalid("invalid scope " + f)
			}
			if !slices.Contains(scopes, f) {
				scopes = append(scopes, f)
			}
		}
	}
	if len(p.Scopes) == 0 {
		scopes = []string{"openid", "profile", "email"}
	}
	p.Scopes = scopes
	def := func(v *string, d string) {
		*v = strings.TrimSpace(*v)
		if *v == "" {
			*v = d
		}
	}
	def(&p.UsernameClaim, "preferred_username")
	def(&p.EmailClaim, "email")
	def(&p.NameClaim, "name")
	def(&p.GroupsClaim, "groups")
	for _, c := range []string{p.UsernameClaim, p.EmailClaim, p.NameClaim, p.GroupsClaim} {
		if !claimRe.MatchString(c) {
			return p, invalid("invalid claim name " + c)
		}
	}
	p.AdminGroup, p.AllowedGroup = strings.TrimSpace(p.AdminGroup), strings.TrimSpace(p.AllowedGroup)
	_ = ctx
	return p, nil
}

// CreateOIDCProvider adds a provider.
func (s *Service) CreateOIDCProvider(ctx context.Context, actor Principal, in OIDCInput) (OIDCProvider, error) {
	p, err := s.normalizeOIDC(ctx, in, nil)
	if err != nil {
		return p, err
	}
	p, err = scanOIDC(s.pool.QueryRow(ctx, `INSERT INTO oidc_providers (id, label, issuer, client_id, client_secret, scopes,
			username_claim, email_claim, name_claim, groups_claim, admin_group, allowed_group, auto_create, link_by_email, trust_email, enabled)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16) RETURNING `+oidcCols,
		p.ID, p.Label, p.Issuer, p.ClientID, p.ClientSecret, p.Scopes, p.UsernameClaim, p.EmailClaim, p.NameClaim, p.GroupsClaim,
		p.AdminGroup, p.AllowedGroup, p.AutoCreate, p.LinkByEmail, p.TrustEmail, p.Enabled))
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return p, invalid("a provider with this id already exists")
	}
	if err == nil {
		_ = s.Audit(ctx, &actor.User.ID, "auth.oidc_created", map[string]any{"provider": p.ID, "issuer": p.Issuer})
	}
	return p, err
}

// UpdateOIDCProvider changes a provider (its id stays).
func (s *Service) UpdateOIDCProvider(ctx context.Context, actor Principal, id string, in OIDCInput) (OIDCProvider, error) {
	old, err := scanOIDC(s.pool.QueryRow(ctx, `SELECT `+oidcCols+` FROM oidc_providers WHERE id = $1`, id))
	if err != nil {
		return old, err
	}
	in.ID = id
	p, err := s.normalizeOIDC(ctx, in, &old)
	if err != nil {
		return p, err
	}
	if old.Enabled && !p.Enabled {
		if err := s.guardExternalAdmin(ctx, id, nil); err != nil {
			return old, err
		}
	}
	p, err = scanOIDC(s.pool.QueryRow(ctx, `UPDATE oidc_providers SET label = $2, issuer = $3, client_id = $4, client_secret = $5,
			scopes = $6, username_claim = $7, email_claim = $8, name_claim = $9, groups_claim = $10, admin_group = $11,
			allowed_group = $12, auto_create = $13, link_by_email = $14, trust_email = $15, enabled = $16, updated_at = now()
		WHERE id = $1 RETURNING `+oidcCols,
		id, p.Label, p.Issuer, p.ClientID, p.ClientSecret, p.Scopes, p.UsernameClaim, p.EmailClaim, p.NameClaim, p.GroupsClaim,
		p.AdminGroup, p.AllowedGroup, p.AutoCreate, p.LinkByEmail, p.TrustEmail, p.Enabled))
	if err == nil {
		s.forgetProvider(old.Issuer)
		_ = s.Audit(ctx, &actor.User.ID, "auth.oidc_updated", map[string]any{"provider": id, "issuer": p.Issuer, "enabled": p.Enabled})
	}
	return p, err
}

// DeleteOIDCProvider removes a provider and the identities linked through it.
func (s *Service) DeleteOIDCProvider(ctx context.Context, actor Principal, id string) error {
	if err := s.guardExternalAdmin(ctx, id, nil); err != nil {
		return err
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM user_identities WHERE kind = 'oidc' AND provider_id = $1`, id); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM oidc_handoffs WHERE provider_id = $1`, id); err != nil {
			return err
		}
		tag, err := tx.Exec(ctx, `DELETE FROM oidc_providers WHERE id = $1`, id)
		if err != nil {
			return err
		}
		if tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return audit(ctx, tx, &actor.User.ID, "auth.oidc_deleted", map[string]any{"provider": id})
	})
}

// guardExternalAdmin refuses a change (removing OIDC provider without, or turning LDAP
// off when ldapOff is set) that would leave password sign-in off with no admin able to
// sign in. withoutOIDC is the provider being removed or disabled ("" for none).
func (s *Service) guardExternalAdmin(ctx context.Context, withoutOIDC string, ldapOff *bool) error {
	a, err := s.SignInSettings(ctx)
	if err != nil || !a.DisablePasswordLogin {
		return err
	}
	l, err := s.LDAPSettings(ctx)
	if err != nil {
		return err
	}
	ldapOn := l.Enabled && (ldapOff == nil || !*ldapOff)
	ok, err := s.externalAdminExists(ctx, withoutOIDC, ldapOn)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNoSSOAdmin
	}
	return nil
}

func (s *Service) externalAdminExists(ctx context.Context, withoutOIDC string, ldapOn bool) (bool, error) {
	var ok bool
	err := s.pool.QueryRow(ctx, `SELECT EXISTS (
		SELECT 1 FROM user_identities i JOIN users u ON u.id = i.user_id
		LEFT JOIN oidc_providers p ON i.kind = 'oidc' AND p.id = i.provider_id
		WHERE u.is_admin AND NOT u.disabled
			AND ((i.kind = 'oidc' AND p.enabled AND p.id <> $1) OR (i.kind = 'ldap' AND $2)))`,
		withoutOIDC, ldapOn).Scan(&ok)
	return ok, err
}

type cachedProvider struct {
	p  *oidc.Provider
	at time.Time
}

func (s *Service) forgetProvider(issuer string) {
	s.oidcMu.Lock()
	delete(s.oidcCache, issuer)
	s.oidcMu.Unlock()
}

// discover fetches (and caches) the provider's discovery document. Its key set is
// kept with it, so signing keys are fetched once and again when an unknown key id
// shows up (key rotation).
func (s *Service) discover(ctx context.Context, issuer string, fresh bool) (*oidc.Provider, error) {
	s.oidcMu.Lock()
	c, ok := s.oidcCache[issuer]
	s.oidcMu.Unlock()
	if ok && !fresh && time.Since(c.at) < oidcDiscoveryTTL {
		return c.p, nil
	}
	dctx, cancel := context.WithTimeout(oidc.ClientContext(ctx, s.httpClient()), 15*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(dctx, issuer)
	var mismatch *oidc.IssuerMismatchError
	if errors.As(err, &mismatch) && strings.TrimRight(mismatch.Discovered, "/") == strings.TrimRight(issuer, "/") {
		// Only a trailing slash differs (Authentik and others add one): use the provider's spelling.
		p, err = oidc.NewProvider(oidc.InsecureIssuerURLContext(dctx, mismatch.Discovered), issuer)
	}
	if err != nil {
		return nil, fmt.Errorf("discovery: %w", err)
	}
	s.oidcMu.Lock()
	if s.oidcCache == nil {
		s.oidcCache = map[string]cachedProvider{}
	}
	s.oidcCache[issuer] = cachedProvider{p: p, at: time.Now()}
	s.oidcMu.Unlock()
	return p, nil
}

// OIDCTestResult summarizes a provider's discovery document.
type OIDCTestResult struct {
	Issuer, AuthURL, TokenURL, UserInfoURL string
	Keys                                   int
	PKCES256                               bool
	Scopes                                 []string
}

// TestOIDC fetches the discovery document and the signing keys of an issuer.
func (s *Service) TestOIDC(ctx context.Context, issuer string) (OIDCTestResult, error) {
	issuer, err := ValidateIssuer(issuer)
	if err != nil {
		return OIDCTestResult{}, err
	}
	p, err := s.discover(ctx, issuer, true)
	if err != nil {
		return OIDCTestResult{}, err
	}
	var meta struct {
		Issuer  string   `json:"issuer"`
		JWKS    string   `json:"jwks_uri"`
		Methods []string `json:"code_challenge_methods_supported"`
		Scopes  []string `json:"scopes_supported"`
	}
	if err := p.Claims(&meta); err != nil {
		return OIDCTestResult{}, err
	}
	res := OIDCTestResult{Issuer: meta.Issuer, AuthURL: p.Endpoint().AuthURL, TokenURL: p.Endpoint().TokenURL,
		UserInfoURL: p.UserInfoEndpoint(), PKCES256: slices.Contains(meta.Methods, "S256"), Scopes: meta.Scopes}
	if meta.JWKS == "" {
		return res, errors.New("the discovery document has no jwks_uri")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, meta.JWKS, nil)
	if err != nil {
		return res, err
	}
	resp, err := s.httpClient().Do(req)
	if err != nil {
		return res, fmt.Errorf("signing keys: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	var jwks struct {
		Keys []json.RawMessage `json:"keys"`
	}
	if resp.StatusCode != http.StatusOK {
		return res, fmt.Errorf("signing keys: %s", resp.Status)
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&jwks); err != nil {
		return res, fmt.Errorf("signing keys: %w", err)
	}
	res.Keys = len(jwks.Keys)
	return res, nil
}

// AuthorizeRequest starts a sign-in.
type AuthorizeRequest struct {
	ProviderID string
	Client     string // web, app or link
	// App flows: the app's redirect URI and its PKCE S256 challenge.
	AppRedirect, AppChallenge string
	// Link flows: the signed-in user to link to.
	LinkUser *uuid.UUID
}

func randomToken() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func s256(v string) string {
	sum := sha256.Sum256([]byte(v))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

var challengeRe = regexp.MustCompile(`^[A-Za-z0-9_-]{43}$`)

func (s *Service) oauthConfig(ctx context.Context, p OIDCProvider) (*oauth2.Config, *oidc.Provider, error) {
	a, err := s.SignInSettings(ctx)
	if err != nil {
		return nil, nil, err
	}
	if a.PublicURL == "" {
		return nil, nil, invalid("set the server's public address first (Settings → Sign-in)")
	}
	op, err := s.discover(ctx, p.Issuer, false)
	if err != nil {
		return nil, nil, err
	}
	return oauthConfigFor(p, op, a.PublicURL), op, nil
}

func oauthConfigFor(p OIDCProvider, op *oidc.Provider, publicURL string) *oauth2.Config {
	return &oauth2.Config{ClientID: p.ClientID, ClientSecret: p.ClientSecret, Endpoint: op.Endpoint(),
		RedirectURL: OIDCRedirectURI(publicURL, p.ID), Scopes: p.Scopes}
}

// OIDCAuthorize creates a pending sign-in and returns the provider URL plus the value
// of the cookie that binds the sign-in to this browser.
func (s *Service) OIDCAuthorize(ctx context.Context, r AuthorizeRequest) (authURL, binding string, err error) {
	switch r.Client {
	case "web":
	case "link":
		if r.LinkUser == nil {
			return "", "", ErrUnauthenticated
		}
	case "app":
		if r.AppRedirect != AppRedirectURI {
			return "", "", invalid("unknown app redirect URI")
		}
		if !challengeRe.MatchString(r.AppChallenge) {
			return "", "", invalid("the app must send a PKCE S256 code challenge")
		}
	default:
		return "", "", invalid("unknown client")
	}
	p, err := scanOIDC(s.pool.QueryRow(ctx, `SELECT `+oidcCols+` FROM oidc_providers WHERE id = $1 AND enabled`, r.ProviderID))
	if err != nil {
		return "", "", err
	}
	cfg, _, err := s.oauthConfig(ctx, p)
	if err != nil {
		return "", "", err
	}
	state, err := randomToken()
	if err != nil {
		return "", "", err
	}
	nonce, err := randomToken()
	if err != nil {
		return "", "", err
	}
	if binding, err = randomToken(); err != nil {
		return "", "", err
	}
	verifier := oauth2.GenerateVerifier()
	if _, err := s.pool.Exec(ctx, `DELETE FROM oidc_flows WHERE expires_at < now()`); err != nil {
		return "", "", err
	}
	if _, err := s.pool.Exec(ctx, `INSERT INTO oidc_flows (state_hash, binding_hash, provider_id, nonce, pkce_verifier, client,
			app_redirect, app_challenge, link_user_id, expires_at) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		hashToken(state), hashToken(binding), p.ID, nonce, verifier, r.Client, r.AppRedirect, r.AppChallenge, r.LinkUser,
		s.now().Add(oidcFlowTTL)); err != nil {
		return "", "", err
	}
	return cfg.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(verifier)), binding, nil
}

// CallbackResult tells the API layer where to send the browser.
type CallbackResult struct {
	Client      string // web, app or link ("" when the flow is unknown)
	AppRedirect string
	// Web sign-in: the new session. App sign-in: the one-time code for the app.
	Principal   Principal
	Token       string
	HandoffCode string
}

// OIDCCallback finishes a sign-in: it consumes the pending flow (single use), checks
// the browser binding, exchanges the code with the PKCE verifier and verifies the ID
// token (signature, issuer, audience, expiry, nonce) before mapping the account.
func (s *Service) OIDCCallback(ctx context.Context, providerID, code, state, binding, idpError string, dev Device) (CallbackResult, error) {
	var res CallbackResult
	if state == "" {
		return res, ErrInvalidState
	}
	var (
		bindingHash     []byte
		nonce, verifier string
		challenge       string
		linkUser        *uuid.UUID
		fresh           bool
	)
	// Deleting the flow makes the state single use, whatever happens next.
	err := s.pool.QueryRow(ctx, `DELETE FROM oidc_flows WHERE state_hash = $1 AND provider_id = $2
		RETURNING binding_hash, nonce, pkce_verifier, client, app_redirect, app_challenge, link_user_id, expires_at > $3`,
		hashToken(state), providerID, s.now()).Scan(&bindingHash, &nonce, &verifier, &res.Client, &res.AppRedirect, &challenge, &linkUser, &fresh)
	if errors.Is(err, pgx.ErrNoRows) {
		return res, ErrInvalidState
	}
	if err != nil {
		return res, err
	}
	if !fresh {
		return res, ErrInvalidState
	}
	// The binding cookie must come from the browser that started the sign-in (login CSRF).
	if binding == "" || subtle.ConstantTimeCompare(hashToken(binding), bindingHash) != 1 {
		return res, ErrInvalidState
	}
	if idpError != "" {
		return res, ErrProviderFailed
	}
	if code == "" {
		return res, ErrInvalidState
	}
	p, err := scanOIDC(s.pool.QueryRow(ctx, `SELECT `+oidcCols+` FROM oidc_providers WHERE id = $1 AND enabled`, providerID))
	if err != nil {
		return res, err
	}
	acct, err := s.verifyCode(ctx, p, code, verifier, nonce)
	if err != nil {
		return res, err
	}
	switch res.Client {
	case "link":
		acct.LinkUser = linkUser
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			_, err := s.resolveExternal(ctx, tx, acct)
			return err
		})
		return res, err
	case "app":
		res.HandoffCode, err = randomToken()
		if err != nil {
			return res, err
		}
		err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
			u, err := s.resolveExternal(ctx, tx, acct)
			if err != nil {
				return err
			}
			if _, err := tx.Exec(ctx, `DELETE FROM oidc_handoffs WHERE expires_at < now()`); err != nil {
				return err
			}
			_, err = tx.Exec(ctx, `INSERT INTO oidc_handoffs (code_hash, user_id, provider_id, challenge, expires_at)
				VALUES ($1, $2, $3, $4, $5)`, hashToken(res.HandoffCode), u.ID, p.ID, challenge, s.now().Add(oidcHandoffTTL))
			return err
		})
		return res, err
	default:
		res.Principal, res.Token, err = s.externalSignIn(ctx, acct, dev, nil)
		return res, err
	}
}

// verifyCode exchanges the authorization code and verifies the ID token.
func (s *Service) verifyCode(ctx context.Context, p OIDCProvider, code, verifier, nonce string) (externalAccount, error) {
	cfg, op, err := s.oauthConfig(ctx, p)
	if err != nil {
		return externalAccount{}, err
	}
	return s.exchangeAndVerify(ctx, cfg, op, p, code, verifier, nonce)
}

// exchangeAndVerify redeems the code at the token endpoint and checks the ID token.
func (s *Service) exchangeAndVerify(ctx context.Context, cfg *oauth2.Config, op *oidc.Provider, p OIDCProvider,
	code, verifier, nonce string) (externalAccount, error) {
	hctx := context.WithValue(ctx, oauth2.HTTPClient, s.httpClient())
	tok, err := cfg.Exchange(hctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		slog.WarnContext(ctx, "oidc: code exchange failed", "provider", p.ID, "err", err)
		return externalAccount{}, ErrProviderFailed
	}
	raw, _ := tok.Extra("id_token").(string)
	if raw == "" {
		slog.WarnContext(ctx, "oidc: token response has no id_token", "provider", p.ID)
		return externalAccount{}, ErrProviderFailed
	}
	idt, err := op.Verifier(&oidc.Config{ClientID: p.ClientID}).Verify(hctx, raw)
	if err != nil {
		slog.WarnContext(ctx, "oidc: ID token rejected", "provider", p.ID, "err", err)
		return externalAccount{}, ErrProviderFailed
	}
	if subtle.ConstantTimeCompare([]byte(idt.Nonce), []byte(nonce)) != 1 {
		slog.WarnContext(ctx, "oidc: ID token nonce mismatch", "provider", p.ID)
		return externalAccount{}, ErrProviderFailed
	}
	claims := map[string]any{}
	if err := idt.Claims(&claims); err != nil {
		return externalAccount{}, err
	}
	if azp, ok := claims["azp"].(string); ok && azp != "" && azp != p.ClientID {
		slog.WarnContext(ctx, "oidc: ID token azp is another client", "provider", p.ID)
		return externalAccount{}, ErrProviderFailed
	}
	// Some providers (Authelia, Entra for groups) put claims only in UserInfo.
	if op.UserInfoEndpoint() != "" {
		if ui, err := op.UserInfo(hctx, oauth2.StaticTokenSource(tok)); err == nil && ui.Subject == idt.Subject {
			extra := map[string]any{}
			if ui.Claims(&extra) == nil {
				for k, v := range extra {
					if _, ok := claims[k]; !ok {
						claims[k] = v
					}
				}
			}
		} else if err != nil {
			slog.InfoContext(ctx, "oidc: userinfo unavailable; using ID token claims", "provider", p.ID, "err", err)
		}
	}
	return accountFromClaims(p, idt.Subject, claims)
}

// accountFromClaims maps verified claims to an external account.
func accountFromClaims(p OIDCProvider, subject string, claims map[string]any) (externalAccount, error) {
	groups := claimStrings(claims, p.GroupsClaim)
	if p.AllowedGroup != "" && !slices.Contains(groups, p.AllowedGroup) {
		return externalAccount{}, ErrNotAllowed
	}
	a := externalAccount{Kind: "oidc", ProviderID: p.ID, Subject: subject,
		Username: claimString(claims, p.UsernameClaim), Email: claimString(claims, p.EmailClaim),
		DisplayName: claimString(claims, p.NameClaim), LinkByEmail: p.LinkByEmail, AutoCreate: p.AutoCreate}
	a.EmailVerified = a.Email != "" && (p.TrustEmail || claimBool(claims, "email_verified"))
	if p.AdminGroup != "" {
		v := slices.Contains(groups, p.AdminGroup)
		a.IsAdmin = &v
	}
	a.Label = a.Email
	if a.Label == "" {
		a.Label = a.Username
	}
	if a.Label == "" {
		a.Label = a.DisplayName
	}
	return a, nil
}

// claim looks a claim up by name, or by a dotted path into nested objects
// (e.g. realm_access.roles) when there is no claim with the full name.
func claim(claims map[string]any, name string) any {
	if v, ok := claims[name]; ok {
		return v
	}
	var cur any = claims
	for _, part := range strings.Split(name, ".") {
		m, ok := cur.(map[string]any)
		if !ok {
			return nil
		}
		cur = m[part]
	}
	return cur
}

func claimString(claims map[string]any, name string) string {
	if s, ok := claim(claims, name).(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func claimStrings(claims map[string]any, name string) []string {
	switch v := claim(claims, name).(type) {
	case string:
		return []string{v}
	case []any:
		out := make([]string, 0, len(v))
		for _, x := range v {
			if s, ok := x.(string); ok {
				out = append(out, s)
			}
		}
		return out
	}
	return nil
}

func claimBool(claims map[string]any, name string) bool {
	switch v := claim(claims, name).(type) {
	case bool:
		return v
	case string:
		return strings.EqualFold(v, "true")
	}
	return false
}

// OIDCExchange redeems an app's one-time code with its PKCE verifier.
func (s *Service) OIDCExchange(ctx context.Context, code, verifier string, dev Device) (Principal, string, error) {
	var (
		userID     uuid.UUID
		providerID string
		challenge  string
	)
	err := s.pool.QueryRow(ctx, `DELETE FROM oidc_handoffs WHERE code_hash = $1 AND expires_at > $2
		RETURNING user_id, provider_id, challenge`, hashToken(code), s.now()).Scan(&userID, &providerID, &challenge)
	if errors.Is(err, pgx.ErrNoRows) {
		return Principal{}, "", ErrInvalidState
	}
	if err != nil {
		return Principal{}, "", err
	}
	if subtle.ConstantTimeCompare([]byte(s256(verifier)), []byte(challenge)) != 1 {
		_ = audit(ctx, s.pool, &userID, "login.failed", map[string]any{"reason": "oidc_bad_verifier", "provider": providerID})
		return Principal{}, "", ErrInvalidState
	}
	var p Principal
	var token string
	now := s.now()
	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		u, err := scanUser(tx.QueryRow(ctx, `SELECT `+userCols+` FROM users WHERE id = $1 FOR UPDATE`, userID))
		if err != nil {
			return err
		}
		if u.Disabled {
			return ErrInvalidCredentials
		}
		if _, err := tx.Exec(ctx, `UPDATE users SET last_login_at = $2 WHERE id = $1`, u.ID, now); err != nil {
			return err
		}
		u.LastLoginAt = &now
		if p, token, err = createSession(ctx, tx, u, dev, now); err != nil {
			return err
		}
		return audit(ctx, tx, &u.ID, "login.succeeded", map[string]any{"session_id": p.SessionID, "device": dev.Name,
			"method": "oidc", "provider": providerID})
	})
	return p, token, err
}
