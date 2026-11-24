package secretbox

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSealOpenAndKeyFile(t *testing.T) {
	dir := t.TempDir()
	b, err := Load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Join(dir, KeyFile))
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf("key file: %v %v", st, err)
	}
	s, err := b.Seal("tpdb-secret-token")
	if err != nil || strings.Contains(s, "tpdb-secret-token") || !strings.HasPrefix(s, "v1:") {
		t.Fatalf("sealed %q %v", s, err)
	}
	again, _ := b.Seal("tpdb-secret-token")
	if again == s {
		t.Fatal("same ciphertext twice (nonce reused)")
	}
	// The same folder gives the same key.
	b2, err := Load("", dir)
	if err != nil {
		t.Fatal(err)
	}
	if p, err := b2.Open(s); err != nil || p != "tpdb-secret-token" {
		t.Fatalf("open: %q %v", p, err)
	}
	other, _ := Load("some other key", "")
	if _, err := other.Open(s); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("another key opened it: %v", err)
	}
	if _, err := b.Open("plaintext"); !errors.Is(err, ErrCorrupt) {
		t.Fatalf("plain value: %v", err)
	}
	if v, err := b.Seal(""); v != "" || err != nil {
		t.Fatal("empty value")
	}
}
