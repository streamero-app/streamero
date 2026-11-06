package auth

import (
	"strings"
	"testing"
)

func TestHashAndVerifyPassword(t *testing.T) {
	h, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(h, "$argon2id$v=19$m=65536,t=3,p=2$") {
		t.Fatalf("unexpected hash format: %s", h)
	}
	if ok, err := VerifyPassword("correct horse battery staple", h); err != nil || !ok {
		t.Fatalf("correct password rejected: ok=%v err=%v", ok, err)
	}
	if ok, _ := VerifyPassword("wrong", h); ok {
		t.Fatal("wrong password accepted")
	}
	h2, _ := HashPassword("correct horse battery staple")
	if h == h2 {
		t.Fatal("hashes must be salted")
	}
}

func TestVerifyPasswordRejectsMalformedHashes(t *testing.T) {
	for _, h := range []string{"", "plain", "$argon2i$v=19$m=1,t=1,p=1$c2FsdA$aGFzaA", "$argon2id$v=1$m=1,t=1,p=1$c2FsdA$aGFzaA",
		"$argon2id$v=19$m=x$c2FsdA$aGFzaA", "$argon2id$v=19$m=1,t=1,p=1$!!$aGFzaA"} {
		if ok, err := VerifyPassword("x", h); ok || err == nil {
			t.Errorf("%q: ok=%v err=%v, want rejection", h, ok, err)
		}
	}
}

func TestSetupCode(t *testing.T) {
	code, err := NewSetupCode()
	if err != nil {
		t.Fatal(err)
	}
	if len(code) != 9 || code[4] != '-' {
		t.Fatalf("bad format %q", code)
	}
	for _, c := range strings.ReplaceAll(code, "-", "") {
		if !strings.ContainsRune(setupAlphabet, c) {
			t.Fatalf("unexpected character %q in %q", c, code)
		}
	}
	lower := strings.ToLower(code)
	if !setupCodeMatches(lower, code) || !setupCodeMatches(strings.ReplaceAll(code, "-", " "), code) {
		t.Error("code must match case-insensitively and ignore separators")
	}
	if setupCodeMatches("", "") || setupCodeMatches("AAAA-AAAA", code) && code != "AAAA-AAAA" {
		t.Error("empty or wrong code must not match")
	}
}

func TestNormalizeUsername(t *testing.T) {
	for in, want := range map[string]string{"  Ana ": "Ana", "mislav.z": "mislav.z", "Đuro_1@kuća": "Đuro_1@kuća"} {
		got, err := NormalizeUsername(in)
		if err != nil || got != want {
			t.Errorf("%q -> %q, %v; want %q", in, got, err, want)
		}
	}
	for _, in := range []string{"", "   ", "a/b", "<script>", strings.Repeat("x", 65)} {
		if _, err := NormalizeUsername(in); err == nil {
			t.Errorf("%q accepted", in)
		}
	}
}

func TestTokenHashing(t *testing.T) {
	tok, hash, err := newToken()
	if err != nil {
		t.Fatal(err)
	}
	if len(tok) < 40 {
		t.Fatalf("token too short: %d", len(tok))
	}
	if string(hashToken(tok)) != string(hash) {
		t.Fatal("hash mismatch")
	}
}
