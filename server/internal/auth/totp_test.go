package auth

import (
	"strings"
	"testing"
	"time"
)

// RFC 6238 Appendix B test vectors (SHA-1 secret "12345678901234567890"), last 6 digits.
func TestTOTPRFC6238Vectors(t *testing.T) {
	secret := []byte("12345678901234567890")
	for unix, want := range map[int64]string{
		59: "287082", 1111111109: "081804", 1111111111: "050471",
		1234567890: "005924", 2000000000: "279037", 20000000000: "353130",
	} {
		if got := totpCode(secret, totpStep(time.Unix(unix, 0))); got != want {
			t.Errorf("t=%d: %s, want %s", unix, got, want)
		}
	}
}

func TestVerifyTOTPWindowAndReplay(t *testing.T) {
	secret := []byte("12345678901234567890")
	now := time.Unix(1111111111, 0)
	step := totpStep(now)

	if got, ok := verifyTOTP(secret, totpCode(secret, step), now, 0); !ok || got != step {
		t.Fatal("current code rejected")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, step-1), now, 0); !ok {
		t.Fatal("previous step (clock drift) rejected")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, step-2), now, 0); ok {
		t.Fatal("code two steps old accepted")
	}
	if _, ok := verifyTOTP(secret, totpCode(secret, step), now, step); ok {
		t.Fatal("replayed code accepted")
	}
	if _, ok := verifyTOTP(secret, "12345", now, 0); ok {
		t.Fatal("short code accepted")
	}
	code := totpCode(secret, step)
	if _, ok := verifyTOTP(secret, code[:3]+" "+code[3:], now, 0); !ok {
		t.Fatal("code with space rejected")
	}
}

func TestTOTPURIAndRecoveryCodes(t *testing.T) {
	uri := totpURI("Streamero", "ana marić", []byte("12345678901234567890"))
	if !strings.HasPrefix(uri, "otpauth://totp/Streamero:ana%20mari%C4%87?") || !strings.Contains(uri, "secret=GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ") {
		t.Fatalf("uri: %s", uri)
	}
	codes, err := newRecoveryCodes(10)
	if err != nil {
		t.Fatal(err)
	}
	seen := map[string]bool{}
	for _, c := range codes {
		if len(c) != 14 || seen[c] {
			t.Fatalf("bad or duplicate code %q", c)
		}
		seen[c] = true
	}
	if normalizeRecoveryCode(" K7FQ-29DM-XP4H ") != "k7fq29dmxp4h" {
		t.Fatal("normalization")
	}
}
