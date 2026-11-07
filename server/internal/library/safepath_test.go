package library

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestResolveSafePath(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "media")
	outside := filepath.Join(base, "secret")
	for _, d := range []string{filepath.Join(root, "movies", "A (2020)"), outside} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink(outside, filepath.Join(root, "escape")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "movies"), filepath.Join(root, "alias")); err != nil {
		t.Fatal(err)
	}
	roots := Roots{root}

	ok := []string{root, filepath.Join(root, "movies"), filepath.Join(root, "movies", "A (2020)"), filepath.Join(root, "alias")}
	for _, p := range ok {
		if _, err := ResolveSafePath(roots, p); err != nil {
			t.Errorf("%s rejected: %v", p, err)
		}
	}
	bad := []string{
		"", "movies", outside, base,
		filepath.Join(root, "..", "secret"),
		filepath.Join(root, "movies", "..", "..", "secret"),
		filepath.Join(root, "escape"), // symlink pointing outside
		root + "-other",
		filepath.Join(root, "movies") + "\x00",
	}
	for _, p := range bad {
		if _, err := ResolveSafePath(roots, p); err == nil {
			t.Errorf("%q accepted", p)
		}
	}
	if _, err := ResolveSafePath(roots, filepath.Join(root, "missing")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("missing path: %v", err)
	}
	if _, err := ParseRoots("relative/path"); err == nil {
		t.Error("relative root accepted")
	}
}
