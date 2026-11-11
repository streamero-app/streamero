package app

import (
	"encoding/json"
	"html"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
)

// TestKeycloakSignInIntegration signs in through a real Keycloak (opt-in, not in CI):
//
//	docker run -d --name kc -v $PWD/testdata:/opt/keycloak/data/import:ro \
//	  -e KC_BOOTSTRAP_ADMIN_USERNAME=admin -e KC_BOOTSTRAP_ADMIN_PASSWORD=admin \
//	  quay.io/keycloak/keycloak:26.4.0 start-dev --import-realm
//
// and run the test inside that container's network (so the issuer is on localhost)
// with STREAMERO_TEST_KEYCLOAK_URL=http://localhost:8080. testdata/keycloak-realm.json
// sets up realm "streamero", confidential
// client "streamero" (secret "kc-secret", redirect URI
// http://127.0.0.1:18787/api/v1/auth/oidc/keycloak/callback, a group membership mapper
// "groups"), user "kcadmin"/"kc-pass" in group "media-admins". The test server listens
// on 127.0.0.1:18787 so the redirect URI is fixed.
func TestKeycloakSignInIntegration(t *testing.T) {
	kc := os.Getenv("STREAMERO_TEST_KEYCLOAK_URL")
	if kc == "" {
		t.Skip("STREAMERO_TEST_KEYCLOAK_URL not set")
	}
	unstarted := httptest.NewUnstartedServer(nil)
	ln, err := net.Listen("tcp", "127.0.0.1:18787")
	if err != nil {
		t.Fatal(err)
	}
	srv := newAuthTestServer(t)
	unstarted.Listener = ln
	unstarted.Config.Handler = srv.Config.Handler
	unstarted.Start()
	defer unstarted.Close()
	base := unstarted.URL

	admin := newClient(t, unstarted)
	setupAdmin(t, admin)
	if s := admin.do("PUT", "/settings/auth", map[string]any{"publicUrl": base, "disablePasswordLogin": false}, nil); s != 200 {
		t.Fatalf("settings: %d", s)
	}
	issuer := kc + "/realms/streamero"
	var test map[string]any
	if s := admin.do("POST", "/settings/auth/oidc-test", map[string]any{"issuer": issuer}, &test); s != 200 {
		t.Fatalf("test connection: %d %v", s, test)
	}
	t.Logf("Keycloak discovery: %v", test)
	if s := admin.do("POST", "/settings/auth/oidc", map[string]any{"id": "keycloak", "label": "Keycloak", "issuer": issuer,
		"clientId": "streamero", "clientSecret": "kc-secret", "autoCreate": true, "adminGroup": "media-admins"}, nil); s != 200 {
		t.Fatalf("create provider: %d", s)
	}

	jar, _ := cookiejar.New(nil)
	b := &browser{t: t, http: &http.Client{Jar: jar, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
	// Our authorize → Keycloak's login page.
	_, loc, _ := b.request("GET", base+"/api/v1/auth/oidc/keycloak/authorize", nil)
	if !strings.HasPrefix(loc, issuer) {
		t.Fatalf("authorize redirected to %q", loc)
	}
	status, _, page := b.request("GET", loc, nil)
	m := regexp.MustCompile(`<form[^>]+id="kc-form-login"[^>]+action="([^"]+)"`).FindSubmatch(page)
	if status != 200 || m == nil {
		t.Fatalf("no Keycloak login form (HTTP %d)", status)
	}
	form := url.Values{"username": {"kcadmin"}, "password": {"kc-pass"}, "credentialId": {""}}
	req, _ := http.NewRequest("POST", html.UnescapeString(string(m[1])), strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := b.http.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	callback := resp.Header.Get("Location")
	if !strings.HasPrefix(callback, base+"/api/v1/auth/oidc/keycloak/callback?") {
		t.Fatalf("Keycloak login redirected to %q (HTTP %d)", callback, resp.StatusCode)
	}
	_, final, _ := b.request("GET", callback, nil)
	if final != "/" {
		t.Fatalf("callback redirected to %q", final)
	}
	_, _, raw := b.request("GET", base+"/api/v1/auth/me", nil)
	var me map[string]any
	_ = json.Unmarshal(raw, &me)
	if me["username"] != "kcadmin" || me["isAdmin"] != true || me["email"] != "kcadmin@example.org" {
		t.Fatalf("me = %s", raw)
	}
	if _, loc, _ := b.request("GET", callback, nil); loc != "/login?sso_error=invalid_state" {
		t.Fatalf("replay: %q", loc)
	}
}
