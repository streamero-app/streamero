package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"math/big"
	"strings"
)

// newToken returns a random session token and the SHA-256 hash that is stored.
func newToken() (token string, hash []byte, err error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(buf)
	return token, hashToken(token), nil
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}

// Setup codes avoid look-alike characters (0/O, 1/I/L) so they can be read from a log.
const setupAlphabet = "ABCDEFGHJKMNPQRSTUVWXYZ23456789"

// NewSetupCode returns a random code formatted as XXXX-XXXX (~39 bits).
func NewSetupCode() (string, error) {
	var b strings.Builder
	alphabetLen := big.NewInt(int64(len(setupAlphabet)))
	for i := range 8 {
		if i == 4 {
			b.WriteByte('-')
		}
		n, err := rand.Int(rand.Reader, alphabetLen) // uniform, no modulo bias
		if err != nil {
			return "", err
		}
		b.WriteByte(setupAlphabet[n.Int64()])
	}
	return b.String(), nil
}

// setupCodeMatches compares codes case-insensitively, ignoring spaces and dashes.
func setupCodeMatches(got, want string) bool {
	norm := func(s string) []byte {
		s = strings.ToUpper(s)
		s = strings.NewReplacer("-", "", " ", "").Replace(s)
		return []byte(s)
	}
	g, w := norm(got), norm(want)
	return len(w) > 0 && subtle.ConstantTimeCompare(g, w) == 1
}
