package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"golang.org/x/oauth2"

	"github.com/streamero-app/streamero/server/internal/auth/oidctest"
)

const testRedirect = "https://media.example.com/api/v1/auth/oidc/fake/callback"

// signIn runs code exchange + ID token verification against the fake provider.
func signIn(t *testing.T, s *Service, fp *oidctest.Provider, p OIDCProvider, mutateVerifier bool) (externalAccount, error) {
	t.Helper()
	ctx := context.Background()
	op, err := s.discover(ctx, fp.Issuer(), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := oauthConfigFor(p, op, "https://media.example.com")
	verifier := oauth2.GenerateVerifier()
	code := fp.IssueCode("nonce-1", s256(verifier), testRedirect)
	if mutateVerifier {
		verifier = oauth2.GenerateVerifier()
	}
	return s.exchangeAndVerify(ctx, cfg, op, p, code, verifier, "nonce-1")
}

func newFake(t *testing.T) (*Service, *oidctest.Provider, OIDCProvider) {
	t.Helper()
	fp := oidctest.New("streamero", "s3cret")
	t.Cleanup(fp.Close)
	s := &Service{now: time.Now, HTTPClient: fp.Server.Client()}
	p := OIDCProvider{ID: "fake", Label: "Fake", Issuer: fp.Issuer(), ClientID: "streamero", ClientSecret: "s3cret",
		Scopes: []string{"openid", "profile", "email"}, UsernameClaim: "preferred_username", EmailClaim: "email",
		NameClaim: "name", GroupsClaim: "groups", Enabled: true}
	return s, fp, p
}

func TestOIDCHappyPath(t *testing.T) {
	s, fp, p := newFake(t)
	p.AdminGroup = "media-admins"
	fp.SetUser(map[string]any{"sub": "abc", "preferred_username": "ana", "email": "ana@example.com", "email_verified": true,
		"name": "Ana", "groups": []string{"family", "media-admins"}})
	a, err := signIn(t, s, fp, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Subject != "abc" || a.Username != "ana" || a.Email != "ana@example.com" || !a.EmailVerified || a.DisplayName != "Ana" {
		t.Fatalf("account = %+v", a)
	}
	if a.IsAdmin == nil || !*a.IsAdmin {
		t.Fatal("member of the admin group must be admin")
	}
}

func TestOIDCRejectsBadTokens(t *testing.T) {
	cases := []struct {
		name  string
		setup func(fp *oidctest.Provider)
	}{
		{"bad signature", func(fp *oidctest.Provider) { fp.SignWithForeignKey(true) }},
		{"HMAC with client secret", func(fp *oidctest.Provider) { fp.SignWithHMAC(true) }},
		{"wrong audience", func(fp *oidctest.Provider) { fp.Mutate(func(c map[string]any) { c["aud"] = "another-client" }) }},
		{"wrong issuer", func(fp *oidctest.Provider) { fp.Mutate(func(c map[string]any) { c["iss"] = "https://evil.example" }) }},
		{"expired", func(fp *oidctest.Provider) {
			fp.Mutate(func(c map[string]any) { c["exp"] = time.Now().Add(-10 * time.Minute).Unix() })
		}},
		{"nonce mismatch", func(fp *oidctest.Provider) { fp.Mutate(func(c map[string]any) { c["nonce"] = "replayed" }) }},
		{"no nonce", func(fp *oidctest.Provider) { fp.Mutate(func(c map[string]any) { delete(c, "nonce") }) }},
		{"azp of another client", func(fp *oidctest.Provider) {
			fp.Mutate(func(c map[string]any) { c["aud"] = []string{"streamero", "other"}; c["azp"] = "other" })
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s, fp, p := newFake(t)
			tc.setup(fp)
			if _, err := signIn(t, s, fp, p, false); !errors.Is(err, ErrProviderFailed) {
				t.Fatalf("err = %v, want ErrProviderFailed", err)
			}
		})
	}
}

func TestOIDCWrongPKCEVerifierIsRefused(t *testing.T) {
	s, fp, p := newFake(t)
	if _, err := signIn(t, s, fp, p, true); !errors.Is(err, ErrProviderFailed) {
		t.Fatalf("err = %v", err)
	}
}

func TestOIDCKeyRotation(t *testing.T) {
	s, fp, p := newFake(t)
	if _, err := signIn(t, s, fp, p, false); err != nil {
		t.Fatal(err)
	}
	fp.Rotate()
	if _, err := signIn(t, s, fp, p, false); err != nil {
		t.Fatalf("after rotation: %v", err)
	}
	if fp.Requests["/jwks"] < 2 {
		t.Fatalf("JWKS fetched %d times; the new key must be fetched", fp.Requests["/jwks"])
	}
}

func TestOIDCUserInfoClaimsAndAllowedGroup(t *testing.T) {
	s, fp, p := newFake(t)
	p.AllowedGroup = "streamero-users"
	fp.SetUser(map[string]any{"sub": "u1", "preferred_username": "ivo"})
	// Groups only in UserInfo (Authelia's default).
	fp.SetUserInfo(map[string]any{"groups": []string{"streamero-users"}, "email": "ivo@example.com"})
	a, err := signIn(t, s, fp, p, false)
	if err != nil {
		t.Fatal(err)
	}
	if a.Email != "ivo@example.com" || a.EmailVerified {
		t.Fatalf("e-mail from UserInfo without email_verified must not count as verified: %+v", a)
	}
	fp.SetUserInfo(map[string]any{"groups": []string{"other"}})
	if _, err := signIn(t, s, fp, p, false); !errors.Is(err, ErrNotAllowed) {
		t.Fatalf("err = %v, want ErrNotAllowed", err)
	}
}

func TestClaimPaths(t *testing.T) {
	claims := map[string]any{"realm_access": map[string]any{"roles": []any{"admin", "user"}}, "email_verified": "true",
		"groups": "solo"}
	if got := claimStrings(claims, "realm_access.roles"); len(got) != 2 || got[0] != "admin" {
		t.Fatalf("dotted path: %v", got)
	}
	if got := claimStrings(claims, "groups"); len(got) != 1 || got[0] != "solo" {
		t.Fatalf("single string group: %v", got)
	}
	if !claimBool(claims, "email_verified") {
		t.Fatal(`"true" string must count`)
	}
}

func TestUsernameFrom(t *testing.T) {
	for in, want := range map[string]string{"ana": "ana", "ana+media": "ana_media", "  ": "", "Željko K.": "Željko K.",
		"!!!": ""} {
		if got := usernameFrom(in); got != want {
			t.Errorf("usernameFrom(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateIssuer(t *testing.T) {
	for in, ok := range map[string]bool{"https://auth.example.com/realms/home": true, "http://localhost:8080/realms/x": true,
		"http://127.0.0.1:9000": true, "http://auth.example.com": false, "ftp://x": false, "https://x/?a=b": false} {
		if _, err := ValidateIssuer(in); (err == nil) != ok {
			t.Errorf("ValidateIssuer(%q) err = %v", in, err)
		}
	}
}

func TestNormalizePublicURL(t *testing.T) {
	got, err := NormalizePublicURL(" https://media.example.com/ ")
	if err != nil || got != "https://media.example.com" {
		t.Fatalf("%q %v", got, err)
	}
	if _, err := NormalizePublicURL("javascript:alert(1)"); err == nil {
		t.Fatal("must reject non-http URLs")
	}
}
