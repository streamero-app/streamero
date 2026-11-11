package app

import (
	"crypto/tls"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-ldap/ldap/v3"
)

// LDAP sign-in against a real OpenLDAP server (osixia/openldap, CI service container).
// Needs STREAMERO_TEST_LDAP_URL (e.g. ldap://localhost:389); the admin is
// cn=admin,dc=example,dc=org with password "admin".
const (
	ldapBase    = "dc=example,dc=org"
	ldapAdminDN = "cn=admin,dc=example,dc=org"
	ldapAdminPW = "admin"
)

func ldapURL(t *testing.T) string {
	u := os.Getenv("STREAMERO_TEST_LDAP_URL")
	if u == "" {
		t.Skip("STREAMERO_TEST_LDAP_URL not set")
	}
	return u
}

// seedLDAP adds people and groups (idempotent). The container may still be starting.
func seedLDAP(t *testing.T, url string) {
	t.Helper()
	var conn *ldap.Conn
	var err error
	for range 60 {
		if conn, err = ldap.DialURL(url); err == nil {
			if err = conn.Bind(ldapAdminDN, ldapAdminPW); err == nil {
				break
			}
			_ = conn.Close()
		}
		time.Sleep(time.Second)
	}
	if err != nil {
		t.Fatalf("LDAP not reachable: %v", err)
	}
	defer func() { _ = conn.Close() }()
	add := func(dn string, attrs map[string][]string) {
		req := ldap.NewAddRequest(dn, nil)
		for k, v := range attrs {
			req.Attribute(k, v)
		}
		if err := conn.Add(req); err != nil && !ldap.IsErrorWithCode(err, ldap.LDAPResultEntryAlreadyExists) {
			t.Fatalf("add %s: %v", dn, err)
		}
	}
	add("ou=people,"+ldapBase, map[string][]string{"objectClass": {"organizationalUnit"}, "ou": {"people"}})
	add("ou=groups,"+ldapBase, map[string][]string{"objectClass": {"organizationalUnit"}, "ou": {"groups"}})
	person := func(uid, name, pw string) {
		add("uid="+uid+",ou=people,"+ldapBase, map[string][]string{"objectClass": {"inetOrgPerson"}, "uid": {uid},
			"cn": {name}, "sn": {name}, "mail": {uid + "@example.org"}, "userPassword": {pw}})
	}
	person("alice", "Alice Admin", "alice-pass")
	person("bob", "Bob User", "bob-pass")
	person("eve", "Eve Outsider", "eve-pass")
	dn := func(uid string) string { return "uid=" + uid + ",ou=people," + ldapBase }
	add("cn=streamero,ou=groups,"+ldapBase, map[string][]string{"objectClass": {"groupOfNames"}, "cn": {"streamero"},
		"member": {dn("alice"), dn("bob")}})
	add("cn=media-admins,ou=groups,"+ldapBase, map[string][]string{"objectClass": {"groupOfNames"}, "cn": {"media-admins"},
		"member": {dn("alice")}})
}

func ldapSettings(url string) map[string]any {
	return map[string]any{
		"enabled": true, "url": url, "startTls": false, "skipVerify": false,
		"bindDn": ldapAdminDN, "bindPassword": ldapAdminPW, "baseDn": ldapBase,
		"userFilter": "(&(objectClass=inetOrgPerson)(uid={username}))",
		"groupFilter": "(&(objectClass=groupOfNames)(member={dn}))",
		"adminGroup":  "cn=media-admins,ou=groups," + ldapBase, "allowedGroup": "CN=streamero, OU=groups," + ldapBase,
	}
}

func TestLDAPSignInIntegration(t *testing.T) {
	url := ldapURL(t)
	seedLDAP(t, url)
	srv := newAuthTestServer(t)
	admin := newClient(t, srv)
	setupAdmin(t, admin)

	var res map[string]any
	if s := admin.do("POST", "/settings/auth/ldap/test", map[string]any{"settings": ldapSettings(url), "username": "alice"}, &res); s != 200 ||
		res["ok"] != true || res["isAdmin"] != true || res["allowed"] != true || res["email"] != "alice@example.org" {
		t.Fatalf("test connection: %d %v", s, res)
	}
	bad := ldapSettings(url)
	bad["bindPassword"] = "wrong"
	if admin.do("POST", "/settings/auth/ldap/test", map[string]any{"settings": bad}, &res); res["ok"] != false {
		t.Fatalf("a wrong bind password must fail the test: %v", res)
	}
	// StartTLS verifies the certificate by default: the container's self-signed one is refused.
	tlsOn := ldapSettings(url)
	tlsOn["startTls"] = true
	if admin.do("POST", "/settings/auth/ldap/test", map[string]any{"settings": tlsOn}, &res); res["ok"] != false ||
		!strings.Contains(strings.ToLower(res["message"].(string)), "certificate") {
		t.Fatalf("StartTLS with an untrusted certificate must fail: %v", res)
	}
	if probe, err := ldap.DialURL(url); err == nil {
		// Only when the server offers StartTLS at all can skipping verification succeed.
		if probe.StartTLS(&tls.Config{InsecureSkipVerify: true}) == nil { // #nosec G402 -- test probe
			tlsOn["skipVerify"] = true
			if admin.do("POST", "/settings/auth/ldap/test", map[string]any{"settings": tlsOn}, &res); res["ok"] != true {
				t.Fatalf("StartTLS without verification: %v", res)
			}
		}
		_ = probe.Close()
	}

	var saved map[string]any
	if s := admin.do("PUT", "/settings/auth/ldap", ldapSettings(url), &saved); s != 200 || saved["bindPasswordSet"] != true {
		t.Fatalf("save: %d %v", s, saved)
	}
	if _, ok := saved["bindPassword"]; ok {
		t.Fatal("the bind password must never be returned")
	}
	// Saving without a password keeps the stored one.
	keep := ldapSettings(url)
	delete(keep, "bindPassword")
	admin.do("PUT", "/settings/auth/ldap", keep, &saved)
	if saved["bindPasswordSet"] != true {
		t.Fatal("bind password lost")
	}

	login := func(user, pw string) (int, authResult, apiError) {
		var out struct {
			authResult
			Code string `json:"code"`
		}
		s := newClient(t, srv).do("POST", "/auth/login", map[string]any{"username": user, "password": pw}, &out)
		return s, out.authResult, apiError{Code: out.Code}
	}
	s, r, _ := login("alice", "alice-pass")
	if s != 200 || r.User.Username != "alice" || !r.User.IsAdmin {
		t.Fatalf("alice: %d %+v", s, r)
	}
	if s, _, e := login("alice", "wrong"); s != 401 || e.Code != "invalid_credentials" {
		t.Fatalf("wrong password: %d %v", s, e)
	}
	if s, _, _ := login("Alice", "alice-pass"); s != 200 {
		t.Fatalf("usernames are case-insensitive: %d", s)
	}
	if s, r, _ := login("bob", "bob-pass"); s != 200 || r.User.IsAdmin {
		t.Fatalf("bob: %d %+v", s, r)
	}
	if s, _, _ := login("eve", "eve-pass"); s != 401 {
		t.Fatalf("eve is not in the allowed group: %d", s)
	}
	// No anonymous/unauthenticated bind and no filter injection.
	for _, u := range []string{"bob", "*", "bob)(uid=*", "*)(|(uid=*"} {
		if s, _, _ := login(u, ""); s == 200 {
			t.Fatalf("empty password for %q signed in", u)
		}
		if u != "bob" {
			if s, _, _ := login(u, "bob-pass"); s == 200 {
				t.Fatalf("filter injection %q signed in", u)
			}
		}
	}
	// No password is stored for directory accounts.
	var hash string
	if err := testPool.QueryRow(t.Context(), `SELECT password_hash FROM users WHERE username = 'alice'`).Scan(&hash); err != nil || hash != "" {
		t.Fatalf("password stored: %q %v", hash, err)
	}
	// Third-party apps sign in with the same fields through the compat API.
	call := newClient(t, srv)
	call.base = srv.URL
	var compat map[string]any
	if s := call.do("POST", "/Users/AuthenticateByName", map[string]any{"Username": "bob", "Pw": "bob-pass"}, &compat); s != 200 ||
		compat["AccessToken"] == "" {
		t.Fatalf("compat login: %d %v", s, compat)
	}
	// Lockout still counts failures of directory accounts.
	for range 5 {
		login("bob", "nope")
	}
	if s, _, e := login("bob", "bob-pass"); s != 429 || e.Code != "account_locked" {
		t.Fatalf("lockout: %d %v", s, e)
	}
	// Turning LDAP off: directory users cannot sign in, local admins still can.
	off := ldapSettings(url)
	off["enabled"] = false
	admin.do("PUT", "/settings/auth/ldap", off, nil)
	if s, _, _ := login("alice", "alice-pass"); s != 401 {
		t.Fatalf("LDAP off: %d", s)
	}
	if s, _, _ := login("admin", "admin-pass-1"); s != 200 {
		t.Fatalf("local admin: %d", s)
	}
}
