// Package secretbox encrypts small secrets (API keys of outside services) before
// they are stored in the database (D-053). The key lives outside the database:
// STREAMERO_SECRET_KEY, or a file in the config folder created on first use, so a
// database dump or backup alone does not reveal the secrets.
package secretbox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// KeyFile is the name of the key file in the config folder.
const KeyFile = "secret.key"

const prefix = "v1:"

// ErrCorrupt is returned for values that cannot be decrypted with this key
// (another server's key, a lost key file).
var ErrCorrupt = errors.New("secretbox: the stored secret cannot be decrypted (the key changed); enter it again")

// Box seals and opens secrets with one key.
type Box struct{ aead cipher.AEAD }

// New returns a box for a 32-byte key.
func New(key []byte) (*Box, error) {
	if len(key) != 32 {
		return nil, errors.New("secretbox: key must be 32 bytes")
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Load returns the box of this server: from env (any text, hashed to 32 bytes)
// when set, otherwise from <configDir>/secret.key, which is created (0600) when
// missing. serve and worker share the config folder, so they share the key.
func Load(env, configDir string) (*Box, error) {
	if env = strings.TrimSpace(env); env != "" {
		k := sha256.Sum256([]byte(env))
		return New(k[:])
	}
	if configDir == "" {
		return nil, errors.New("secretbox: no STREAMERO_SECRET_KEY and no config folder")
	}
	p := filepath.Join(configDir, KeyFile)
	raw, err := os.ReadFile(p) // #nosec G304 -- fixed name in the configured folder
	if errors.Is(err, os.ErrNotExist) {
		key := make([]byte, 32)
		if _, err := rand.Read(key); err != nil {
			return nil, err
		}
		enc := base64.StdEncoding.EncodeToString(key)
		f, err := os.OpenFile(p, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600) // #nosec G304 -- fixed name
		if errors.Is(err, os.ErrExist) {
			return Load("", configDir) // the other process created it meanwhile
		}
		if err != nil {
			return nil, fmt.Errorf("secretbox: create %s: %w", p, err)
		}
		if _, err := f.WriteString(enc + "\n"); err != nil {
			_ = f.Close()
			return nil, err
		}
		if err := f.Close(); err != nil {
			return nil, err
		}
		return New(key)
	}
	if err != nil {
		return nil, fmt.Errorf("secretbox: read %s: %w", p, err)
	}
	key, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil || len(key) != 32 {
		return nil, fmt.Errorf("secretbox: %s is not a base64 32-byte key", p)
	}
	return New(key)
}

// Seal encrypts plain ("" stays "").
func (b *Box) Seal(plain string) (string, error) {
	if plain == "" {
		return "", nil
	}
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	ct := b.aead.Seal(nonce, nonce, []byte(plain), []byte(prefix))
	return prefix + base64.StdEncoding.EncodeToString(ct), nil
}

// Open decrypts a value made by Seal ("" stays "").
func (b *Box) Open(sealed string) (string, error) {
	if sealed == "" {
		return "", nil
	}
	if !strings.HasPrefix(sealed, prefix) {
		return "", ErrCorrupt
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(sealed, prefix))
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrCorrupt
	}
	n := b.aead.NonceSize()
	plain, err := b.aead.Open(nil, raw[:n], raw[n:], []byte(prefix))
	if err != nil {
		return "", ErrCorrupt
	}
	return string(plain), nil
}
