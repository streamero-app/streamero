// Package library manages libraries, scans media folders and stores items.
package library

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// ErrOutsideRoots is returned for paths that are not inside a configured media root.
var ErrOutsideRoots = errors.New("path is outside the media folders")

// Roots are the folders libraries may use (STREAMERO_MEDIA_ROOTS).
type Roots []string

// ParseRoots reads a comma-separated list; every root must be absolute.
func ParseRoots(s string) (Roots, error) {
	var out Roots
	for _, r := range strings.Split(s, ",") {
		r = strings.TrimSpace(r)
		if r == "" {
			continue
		}
		if !filepath.IsAbs(r) {
			return nil, errors.New("media root must be absolute: " + r)
		}
		out = append(out, filepath.Clean(r))
	}
	if len(out) == 0 {
		return nil, errors.New("no media roots configured")
	}
	return out, nil
}

// ResolveSafePath cleans p, resolves symlinks and checks that the result lies inside
// one of the roots. Every API input that names a file or folder must pass through it
// (CLAUDE.md). It returns the resolved absolute path.
func ResolveSafePath(roots Roots, p string) (string, error) {
	if p == "" || strings.ContainsRune(p, 0) || !filepath.IsAbs(p) {
		return "", ErrOutsideRoots
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(p))
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		return "", ErrOutsideRoots
	}
	for _, root := range roots {
		realRoot, err := filepath.EvalSymlinks(root)
		if err != nil {
			continue
		}
		if resolved == realRoot || strings.HasPrefix(resolved, realRoot+string(filepath.Separator)) {
			return resolved, nil
		}
	}
	return "", ErrOutsideRoots
}
