package storage

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnsureCacheDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cache", "nested")
	for range 2 { // idempotent
		if err := EnsureCacheDir(dir); err != nil {
			t.Fatal(err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, "CACHEDIR.TAG"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(b), "Signature: 8a477f597d28d172789f06886806bc55") {
		t.Errorf("bad tag: %q", b)
	}
}
