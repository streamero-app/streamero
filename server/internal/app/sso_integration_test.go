package app

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"testing"

	"github.com/streamero-app/streamero/server/internal/auth/oidctest"
)

// browser follows redirects by hand and keeps cookies, like a real browser would.
type browser struct {
	t    *testing.T
	http *http.Client
}

func newBrowser(t *testing.T, fp *oidctest.Provider) *browser {
	jar, _ := cookiejar.New(nil)
	return &browser{t: t, http: &http.Client{Jar: jar, Transport: fp.Server.Client().Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}

func (b *browser) request(method, u string, body any) (int, string, []byte) {
	b.t.Helper()
	var r io.Reader
	if body != nil {
		raw, _ := json.Marshal(body)
		r = bytes.NewReader(raw)
	}
	req, _ := http.NewRequest(method, u, r)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := b.http.Do(req)
	if err != nil {
		b.t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	raw, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Header.Get("Location"), raw
}

// follow opens u and follows absolute redirects until the server sends the browser
// to a relative path or to the app's custom scheme; it returns that last location
// and the callback URL it passed through.
func (b *browser) follow(u string) (final, callback string) {
	b.t.Helper()
	for range 10 {
		if strings.Contains(u, "/callback?") {
			callback = u
		}
		status, loc, body := b.request("GET", u, nil)
		if status != http.StatusFound {
			b.t.Fatalf("GET %s: HTTP %d %s", u, status, body)
		}
		if strings.HasPrefix(loc, "/") || strings.HasPrefix(loc, "streamero:") {
			return loc, callback
		}
		u = loc
	}
	b.t.Fatal("too many redirects")
	return "", ""
}

func pkcePair() (verifier, challenge string) {
	buf := make([]byte, 32)
	_, _ = rand.Read(buf)
	verifier = base64.RawURLEncoding.EncodeToString(buf)
	sum := sha256.Sum256([]byte(verifier))
	return verifier, base64.RawURLEncoding.EncodeToString(sum[:])
}

func TestOIDCSignInIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	admin := newClient(t, srv)
	setupAdmin(t, admin)
	fp := oidctest.New("streamero", "client-s3cret")
	defer fp.Close()
	testState.Auth().HTTPClient = fp.Server.Client()

	// Without a public address SSO cannot build its redirect URI: no buttons yet.
	var methods struct {
		PasswordLogin bool `json:"passwordLogin"`
		Oidc          []struct {
			ID, Label, AuthorizeURL string
		} `json:"oidc"`
	}
	if s := admin.do("PUT", "/settings/auth", map[string]any{"publicUrl": srv.URL + "/", "disablePasswordLogin": false}, nil); s != 200 {
		t.Fatalf("auth settings: %d", s)
	}
	var prov map[string]any
	if s := admin.do("POST", "/settings/auth/oidc", map[string]any{"id": "fake", "label": "Fake IdP", "issuer": fp.Issuer(),
		"clientId": "streamero", "clientSecret": "client-s3cret", "autoCreate": true, "adminGroup": "media-admins"}, &prov); s != 200 {
		t.Fatalf("create provider: %d %v", s, prov)
	}
	if prov["clientSecretSet"] != true || prov["redirectUri"] != srv.URL+"/api/v1/auth/oidc/fake/callback" {
		t.Fatalf("provider = %v", prov)
	}
	_, _, raw := newBrowser(t, fp).request("GET", srv.URL+"/api/v1/settings/auth/oidc", nil)
	if bytes.Contains(raw, []byte("client-s3cret")) {
		t.Fatal("the client secret must never be returned")
	}
	var list []map[string]any
	admin.do("GET", "/settings/auth/oidc", nil, &list)
	if b, _ := json.Marshal(list); bytes.Contains(b, []byte("client-s3cret")) {
		t.Fatal("the client secret must never be returned")
	}
	var test map[string]any
	if s := admin.do("POST", "/settings/auth/oidc-test", map[string]any{"issuer": fp.Issuer()}, &test); s != 200 || test["signingKeys"] != float64(1) ||
		test["pkceS256"] != true {
		t.Fatalf("test: %d %v", s, test)
	}
	if s := admin.do("GET", "/auth/providers", nil, &methods); s != 200 || len(methods.Oidc) != 1 || !methods.PasswordLogin {
		t.Fatalf("providers: %d %+v", s, methods)
	}
	authorize := methods.Oidc[0].AuthorizeURL
	if authorize != srv.URL+"/api/v1/auth/oidc/fake/authorize" {
		t.Fatalf("authorizeUrl = %q", authorize)
	}

	t.Run("web sign-in creates the user and sets the session cookie", func(t *testing.T) {
		fp.SetUser(map[string]any{"sub": "sub-ana", "preferred_username": "ana", "email": "ana@example.com",
			"email_verified": true, "name": "Ana A.", "groups": []string{"media-admins"}})
		b := newBrowser(t, fp)
		final, callback := b.follow(authorize)
		if final != "/" {
			t.Fatalf("final redirect = %q", final)
		}
		if strings.Contains(final, "token") {
			t.Fatal("no token in URLs")
		}
		var me map[string]any
		status, _, raw := b.request("GET", srv.URL+"/api/v1/auth/me", nil)
		_ = json.Unmarshal(raw, &me)
		if status != 200 || me["username"] != "ana" || me["isAdmin"] != true || me["email"] != "ana@example.com" {
			t.Fatalf("me: %d %v", status, me)
		}
		// The state is single use: replaying the callback fails.
		if _, loc, _ := b.request("GET", callback, nil); loc != "/login?sso_error=invalid_state" {
			t.Fatalf("replayed callback went to %q", loc)
		}
	})

	t.Run("a callback from another browser is refused (login CSRF)", func(t *testing.T) {
		attacker := newBrowser(t, fp)
		_, loc, _ := attacker.request("GET", authorize, nil)
		_, cb, _ := attacker.request("GET", loc, nil) // provider approves, sends to callback
		victim := newBrowser(t, fp)
		if _, loc, _ := victim.request("GET", cb, nil); loc != "/login?sso_error=invalid_state" {
			t.Fatalf("callback without the flow cookie went to %q", loc)
		}
	})

	t.Run("app sign-in hands over a one-time code bound to PKCE", func(t *testing.T) {
		fp.SetUser(map[string]any{"sub": "sub-ana", "preferred_username": "ana"})
		verifier, challenge := pkcePair()
		start := authorize + "?" + url.Values{"client": {"app"}, "redirectUri": {"streamero://oidc"}, "codeChallenge": {challenge}}.Encode()
		final, _ := newBrowser(t, fp).follow(start)
		u, err := url.Parse(final)
		if err != nil || u.Scheme != "streamero" || u.Query().Get("code") == "" || u.Query().Get("token") != "" {
			t.Fatalf("app redirect = %q", final)
		}
		app := newClient(t, srv)
		var bad apiError
		other, _ := pkcePair()
		if s := app.do("POST", "/auth/oidc/exchange", map[string]any{"code": u.Query().Get("code"), "codeVerifier": other}, &bad); s != 400 ||
			bad.Code != "invalid_state" {
			t.Fatalf("wrong verifier: %d %v", s, bad)
		}
		// A refused code is burnt; start again and redeem with the right verifier.
		final, _ = newBrowser(t, fp).follow(start)
		u, _ = url.Parse(final)
		var res authResult
		if s := app.do("POST", "/auth/oidc/exchange", map[string]any{"code": u.Query().Get("code"), "codeVerifier": verifier,
			"device": map[string]any{"name": "iPhone"}}, &res); s != 200 || res.Token == "" || res.User.Username != "ana" {
			t.Fatalf("exchange: %d %+v", s, res)
		}
		if s := app.do("POST", "/auth/oidc/exchange", map[string]any{"code": u.Query().Get("code"), "codeVerifier": verifier}, nil); s != 400 {
			t.Fatalf("replayed exchange: %d", s)
		}
		// Unknown app redirect URIs are refused before the provider is contacted.
		bad2 := authorize + "?" + url.Values{"client": {"app"}, "redirectUri": {"https://evil.example/cb"}, "codeChallenge": {challenge}}.Encode()
		if _, loc, _ := newBrowser(t, fp).request("GET", bad2, nil); !strings.HasPrefix(loc, "/login?sso_error=") {
			t.Fatalf("bad redirect went to %q", loc)
		}
	})

	// A local user who set the same e-mail address.
	var maja authResult
	if s := admin.do("POST", "/users", map[string]any{"username": "maja", "password": "maja-pass-1", "isAdmin": false}, &maja.User); s != 201 {
		t.Fatalf("create maja: %d", s)
	}
	majaC := newClient(t, srv)
	if s := majaC.do("POST", "/auth/login", map[string]any{"username": "maja", "password": "maja-pass-1"}, &maja); s != 200 {
		t.Fatalf("maja login: %d", s)
	}
	majaC.token = maja.Token
	if s := majaC.do("PUT", "/auth/me", map[string]any{"email": "Maja@Example.com"}, nil); s != 200 {
		t.Fatalf("maja email: %d", s)
	}

	t.Run("unverified e-mail never links; verified e-mail does", func(t *testing.T) {
		if s := admin.do("PUT", "/settings/auth/oidc/fake", map[string]any{"id": "fake", "label": "Fake IdP", "issuer": fp.Issuer(),
			"clientId": "streamero", "autoCreate": false, "adminGroup": "media-admins"}, &prov); s != 200 || prov["clientSecretSet"] != true {
			t.Fatalf("update provider (secret kept): %d %v", s, prov)
		}
		fp.SetUser(map[string]any{"sub": "sub-maja", "preferred_username": "maja.idp", "email": "maja@example.com", "email_verified": false})
		if final, _ := newBrowser(t, fp).follow(authorize); final != "/login?sso_error=no_account" {
			t.Fatalf("unverified e-mail: %q", final)
		}
		fp.SetUser(map[string]any{"sub": "sub-maja", "preferred_username": "maja.idp", "email": "maja@example.com", "email_verified": true})
		b := newBrowser(t, fp)
		if final, _ := b.follow(authorize); final != "/" {
			t.Fatalf("verified e-mail: %q", final)
		}
		var me map[string]any
		_, _, raw := b.request("GET", srv.URL+"/api/v1/auth/me", nil)
		_ = json.Unmarshal(raw, &me)
		if me["username"] != "maja" || me["isAdmin"] != false {
			t.Fatalf("linked to %v", me)
		}
	})

	t.Run("not in the allowed group", func(t *testing.T) {
		admin.do("PUT", "/settings/auth/oidc/fake", map[string]any{"id": "fake", "label": "Fake IdP", "issuer": fp.Issuer(),
			"clientId": "streamero", "autoCreate": true, "allowedGroup": "streamero"}, nil)
		fp.SetUser(map[string]any{"sub": "sub-x", "preferred_username": "x", "groups": []string{"other"}})
		if final, _ := newBrowser(t, fp).follow(authorize); final != "/login?sso_error=not_allowed" {
			t.Fatalf("final = %q", final)
		}
		admin.do("PUT", "/settings/auth/oidc/fake", map[string]any{"id": "fake", "label": "Fake IdP", "issuer": fp.Issuer(),
			"clientId": "streamero", "autoCreate": true}, nil)
	})

	t.Run("password sign-in can only be turned off with an SSO admin; recovery", func(t *testing.T) {
		var e apiError
		if s := admin.do("PUT", "/settings/auth", map[string]any{"publicUrl": srv.URL, "disablePasswordLogin": true}, &e); s != 409 ||
			e.Code != "no_sso_admin" {
			t.Fatalf("disable without SSO admin: %d %v", s, e)
		}
		// The admin links an account from My Account (needs a browser session).
		b := newBrowser(t, fp)
		if s, _, _ := b.request("POST", srv.URL+"/api/v1/auth/login", map[string]any{"username": "admin", "password": "admin-pass-1",
			"useCookie": true}); s != 200 {
			t.Fatalf("admin cookie login: %d", s)
		}
		var start struct {
			AuthorizationURL string `json:"authorizationUrl"`
		}
		s, _, raw := b.request("POST", srv.URL+"/api/v1/auth/oidc/fake/link", nil)
		_ = json.Unmarshal(raw, &start)
		if s != 200 || start.AuthorizationURL == "" {
			t.Fatalf("link start: %d %s", s, raw)
		}
		fp.SetUser(map[string]any{"sub": "sub-admin", "preferred_username": "boss"})
		if final, _ := b.follow(start.AuthorizationURL); final != "/settings/account?linked=fake" {
			t.Fatalf("link final = %q", final)
		}
		var ids struct {
			HasPassword bool `json:"hasPassword"`
			Identities  []struct {
				ID, Kind, ProviderID string
			} `json:"identities"`
		}
		admin.do("GET", "/auth/identities", nil, &ids)
		if !ids.HasPassword || len(ids.Identities) != 1 || ids.Identities[0].Kind != "oidc" {
			t.Fatalf("identities = %+v", ids)
		}
		// The same external account cannot be linked to a second user.
		mb := newBrowser(t, fp)
		mb.request("POST", srv.URL+"/api/v1/auth/login", map[string]any{"username": "maja", "password": "maja-pass-1", "useCookie": true})
		_, _, raw = mb.request("POST", srv.URL+"/api/v1/auth/oidc/fake/link", nil)
		_ = json.Unmarshal(raw, &start)
		if final, _ := mb.follow(start.AuthorizationURL); final != "/settings/account?sso_error=identity_taken" {
			t.Fatalf("linking a taken identity: %q", final)
		}

		if s := admin.do("PUT", "/settings/auth", map[string]any{"publicUrl": srv.URL, "disablePasswordLogin": true}, nil); s != 200 {
			t.Fatalf("disable password sign-in: %d", s)
		}
		if s := newClient(t, srv).do("POST", "/auth/login", map[string]any{"username": "admin", "password": "admin-pass-1"}, &e); s != 403 ||
			e.Code != "password_login_disabled" {
			t.Fatalf("password login while off: %d %v", s, e)
		}
		admin.do("GET", "/auth/providers", nil, &methods)
		if methods.PasswordLogin {
			t.Fatal("login screen must not offer passwords")
		}
		// Recovery (STREAMERO_PASSWORD_LOGIN_RECOVERY): admins only.
		testState.Auth().PasswordRecovery = true
		if s := newClient(t, srv).do("POST", "/auth/login", map[string]any{"username": "admin", "password": "admin-pass-1"}, nil); s != 200 {
			t.Fatalf("recovery admin login: %d", s)
		}
		if s := newClient(t, srv).do("POST", "/auth/login", map[string]any{"username": "maja", "password": "maja-pass-1"}, nil); s != 403 {
			t.Fatalf("recovery must not open passwords for non-admins: %d", s)
		}
		testState.Auth().PasswordRecovery = false
		// Removing the only provider an admin can use is refused while passwords are off.
		if s := admin.do("DELETE", "/settings/auth/oidc/fake", nil, &e); s != 409 || e.Code != "no_sso_admin" {
			t.Fatalf("delete last SSO provider: %d %v", s, e)
		}
		admin.do("PUT", "/settings/auth", map[string]any{"publicUrl": srv.URL, "disablePasswordLogin": false}, nil)
	})

	t.Run("an SSO-only account cannot unlink its only identity", func(t *testing.T) {
		fp.SetUser(map[string]any{"sub": "sub-ana", "preferred_username": "ana"})
		b := newBrowser(t, fp)
		b.follow(authorize)
		var ids struct {
			HasPassword bool `json:"hasPassword"`
			Identities  []struct {
				ID string `json:"id"`
			} `json:"identities"`
		}
		_, _, raw := b.request("GET", srv.URL+"/api/v1/auth/identities", nil)
		_ = json.Unmarshal(raw, &ids)
		if ids.HasPassword || len(ids.Identities) != 1 {
			t.Fatalf("ana identities = %s", raw)
		}
		if s, _, raw := b.request("DELETE", srv.URL+"/api/v1/auth/identities/"+ids.Identities[0].ID, nil); s != 409 ||
			!bytes.Contains(raw, []byte("last_sign_in_method")) {
			t.Fatalf("unlink only identity: %d %s", s, raw)
		}
		// Nor can it use the password form (it has no password).
		if s := newClient(t, srv).do("POST", "/auth/login", map[string]any{"username": "ana", "password": ""}, nil); s != 401 {
			t.Fatalf("empty password: %d", s)
		}
	})

	t.Run("deleting a provider removes its identities", func(t *testing.T) {
		if s := admin.do("DELETE", "/settings/auth/oidc/fake", nil, nil); s != 204 {
			t.Fatalf("delete: %d", s)
		}
		var n int
		if err := testPool.QueryRow(t.Context(), `SELECT count(*) FROM user_identities WHERE provider_id = 'fake'`).Scan(&n); err != nil || n != 0 {
			t.Fatalf("identities left: %d %v", n, err)
		}
		if _, loc, _ := newBrowser(t, fp).request("GET", authorize, nil); loc != "/login?sso_error=unknown_provider" {
			t.Fatalf("authorize after delete: %q", loc)
		}
	})
}
