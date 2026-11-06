// Package storage handles on-disk layout of config, cache and transcode directories.
package storage

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// cacheDirTag marks a directory as a cache so backup tools skip it.
// See https://bford.info/cachedir/.
const cacheDirTag = "Signature: 8a477f597d28d172789f06886806bc55\n" +
	"# This file is a cache directory tag created by Streamero.\n" +
	"# For information about cache directory tags, see https://bford.info/cachedir/\n"

// EnsureCacheDir creates dir if needed and writes a CACHEDIR.TAG into it.
func EnsureCacheDir(dir string) error {
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return err
	}
	tag := filepath.Join(dir, "CACHEDIR.TAG")
	if _, err := os.Stat(tag); err == nil {
		return nil
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return os.WriteFile(tag, []byte(cacheDirTag), 0o600)
}
