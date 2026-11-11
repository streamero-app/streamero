// Package oidctest is a fake OpenID Connect provider for tests: discovery, JWKS
// (with key rotation), an auto-approving authorization endpoint, a token endpoint
// that checks PKCE and client credentials, and UserInfo.
package oidctest

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"sync"
	"time"

	"github.com/go-jose/go-jose/v4"
)

// Provider is a running fake identity provider.
type Provider struct {
	Server       *httptest.Server
	ClientID     string
	ClientSecret string

	mu      sync.Mutex
	key     *rsa.PrivateKey
	kid     int
	codes   map[string]grant
	tokens  map[string]string // access token -> subject
	claims  map[string]any
	info    map[string]any
	mutate  func(map[string]any)
	signKey *rsa.PrivateKey
	hmac    bool
	// Requests counts calls per path.
	Requests map[string]int
}

type grant struct {
	nonce, challenge, redirect string
	claims                     map[string]any
}

// New starts a TLS provider. Use p.Server.Client() as the HTTP client of the code under test.
func New(clientID, clientSecret string) *Provider {
	p := &Provider{ClientID: clientID, ClientSecret: clientSecret, codes: map[string]grant{}, tokens: map[string]string{},
		Requests: map[string]int{}, claims: map[string]any{"sub": "user-1"}}
	p.rotateLocked()
	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", p.discovery)
	mux.HandleFunc("/jwks", p.jwks)
	mux.HandleFunc("/authorize", p.authorize)
	mux.HandleFunc("/token", p.token)
	mux.HandleFunc("/userinfo", p.userinfo)
	p.Server = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p.mu.Lock()
		p.Requests[r.URL.Path]++
		p.mu.Unlock()
		mux.ServeHTTP(w, r)
	}))
	return p
}

// Issuer is the provider's issuer URL.
func (p *Provider) Issuer() string { return p.Server.URL }

// Close stops the server.
func (p *Provider) Close() { p.Server.Close() }

// SetUser sets the claims of whoever signs in next (sub is required).
func (p *Provider) SetUser(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.claims = claims
}

// SetUserInfo sets extra claims served only by the UserInfo endpoint.
func (p *Provider) SetUserInfo(claims map[string]any) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.info = claims
}

// Mutate changes the ID token claims just before signing (nil = none).
func (p *Provider) Mutate(f func(map[string]any)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.mutate = f
}

// SignWithForeignKey signs ID tokens with a key that is not in the JWKS (same key id).
func (p *Provider) SignWithForeignKey(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.signKey = nil
	if on {
		k, err := rsa.GenerateKey(rand.Reader, 2048)
		if err != nil {
			panic(err)
		}
		p.signKey = k
	}
}

// SignWithHMAC signs ID tokens with HS256 using the client secret (must be refused).
func (p *Provider) SignWithHMAC(on bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.hmac = on
}

// Rotate replaces the signing key (the old one leaves the JWKS).
func (p *Provider) Rotate() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.rotateLocked()
}

func (p *Provider) rotateLocked() {
	k, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	p.key = k
	p.kid++
}

// IssueCode creates an authorization code directly (as if the user approved).
func (p *Provider) IssueCode(nonce, challenge, redirect string) string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.issueLocked(nonce, challenge, redirect)
}

func (p *Provider) issueLocked(nonce, challenge, redirect string) string {
	code := random()
	claims := map[string]any{}
	for k, v := range p.claims {
		claims[k] = v
	}
	p.codes[code] = grant{nonce: nonce, challenge: challenge, redirect: redirect, claims: claims}
	return code
}

func random() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func (p *Provider) discovery(w http.ResponseWriter, _ *http.Request) {
	u := p.Server.URL
	writeJSON(w, http.StatusOK, map[string]any{
		"issuer": u, "authorization_endpoint": u + "/authorize", "token_endpoint": u + "/token",
		"userinfo_endpoint": u + "/userinfo", "jwks_uri": u + "/jwks",
		"response_types_supported": []string{"code"}, "subject_types_supported": []string{"public"},
		"id_token_signing_alg_values_supported": []string{"RS256"},
		"code_challenge_methods_supported":      []string{"S256"},
		"scopes_supported":                      []string{"openid", "profile", "email", "groups"},
	})
}

func (p *Provider) jwks(w http.ResponseWriter, _ *http.Request) {
	p.mu.Lock()
	jwk := jose.JSONWebKey{Key: &p.key.PublicKey, KeyID: strconv.Itoa(p.kid), Algorithm: "RS256", Use: "sig"}
	p.mu.Unlock()
	writeJSON(w, http.StatusOK, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{jwk}})
}

// authorize approves at once and redirects back with a code (no login form).
func (p *Provider) authorize(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("response_type") != "code" || q.Get("client_id") != p.ClientID || q.Get("code_challenge_method") != "S256" ||
		q.Get("code_challenge") == "" || q.Get("state") == "" || q.Get("nonce") == "" {
		http.Error(w, "bad authorization request", http.StatusBadRequest)
		return
	}
	redirect := q.Get("redirect_uri")
	p.mu.Lock()
	code := p.issueLocked(q.Get("nonce"), q.Get("code_challenge"), redirect)
	p.mu.Unlock()
	// #nosec G710 -- test fake; the redirect URI comes from the code under test
	http.Redirect(w, r, redirect+"?"+url.Values{"code": {code}, "state": {q.Get("state")}, "iss": {p.Server.URL}}.Encode(), http.StatusFound)
}

func (p *Provider) token(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil || r.Method != http.MethodPost {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_request"})
		return
	}
	id, secret, ok := r.BasicAuth()
	if !ok {
		id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
	}
	if id != p.ClientID || secret != p.ClientSecret {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_client"})
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	g, ok := p.codes[r.PostForm.Get("code")]
	delete(p.codes, r.PostForm.Get("code")) // single use
	sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
	if !ok || r.PostForm.Get("grant_type") != "authorization_code" || r.PostForm.Get("redirect_uri") != g.redirect ||
		base64.RawURLEncoding.EncodeToString(sum[:]) != g.challenge {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_grant"})
		return
	}
	now := time.Now()
	claims := map[string]any{"iss": p.Server.URL, "aud": p.ClientID, "iat": now.Unix(), "exp": now.Add(5 * time.Minute).Unix(),
		"nonce": g.nonce}
	for k, v := range g.claims {
		claims[k] = v
	}
	if p.mutate != nil {
		p.mutate(claims)
	}
	idToken, err := p.signLocked(claims)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	access := random()
	sub, _ := g.claims["sub"].(string)
	p.tokens[access] = sub
	writeJSON(w, http.StatusOK, map[string]any{"access_token": access, "token_type": "Bearer", "expires_in": 300, "id_token": idToken})
}

func (p *Provider) signLocked(claims map[string]any) (string, error) {
	var key jose.SigningKey
	switch {
	case p.hmac:
		key = jose.SigningKey{Algorithm: jose.HS256, Key: []byte(p.ClientSecret)}
	case p.signKey != nil:
		key = jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.signKey, KeyID: strconv.Itoa(p.kid)}}
	default:
		key = jose.SigningKey{Algorithm: jose.RS256, Key: jose.JSONWebKey{Key: p.key, KeyID: strconv.Itoa(p.kid)}}
	}
	signer, err := jose.NewSigner(key, (&jose.SignerOptions{}).WithType("JWT"))
	if err != nil {
		return "", err
	}
	payload, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	jws, err := signer.Sign(payload)
	if err != nil {
		return "", err
	}
	return jws.CompactSerialize()
}

func (p *Provider) userinfo(w http.ResponseWriter, r *http.Request) {
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	p.mu.Lock()
	defer p.mu.Unlock()
	sub, ok := "", false
	if len(h) > len(prefix) {
		sub, ok = p.tokens[h[len(prefix):]]
	}
	if !ok {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}
	out := map[string]any{"sub": sub}
	for k, v := range p.info {
		out[k] = v
	}
	writeJSON(w, http.StatusOK, out)
}
