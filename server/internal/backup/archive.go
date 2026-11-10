package backup

import (
	"archive/tar"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"strings"
	"time"
)

// FormatVersion is the archive layout written by this server.
const FormatVersion = 1

// appName marks a Streamero archive.
const appName = "streamero"

// Archive entry names and prefixes.
const (
	manifestEntry = "manifest.json"
	dbPrefix      = "db/"
	configPrefix  = "config/"
	avatarPrefix  = "avatars/"
	imagePrefix   = "images/"
)

// maxManifestBytes bounds the manifest a reader accepts.
const maxManifestBytes = 16 << 20

// Manifest is the first entry of every archive.
type Manifest struct {
	Format         int        `json:"format"`
	App            string     `json:"app"`
	ServerVersion  string     `json:"serverVersion"`
	Commit         string     `json:"commit,omitempty"`
	SchemaVersion  int64      `json:"schemaVersion"`
	CreatedAt      time.Time  `json:"createdAt"`
	Kind           string     `json:"kind"`
	ScanRunning    bool       `json:"scanRunning"`
	IncludesImages bool       `json:"includesImages"`
	Tables         []Table    `json:"tables"`
	Skipped        []Skipped  `json:"skipped,omitempty"`
	Sequences      []Sequence `json:"sequences"`
	Files          FileCounts `json:"files"`
}

// Table is one dumped table (COPY text format in db/<name>.tsv).
type Table struct {
	Name    string   `json:"name"`
	Columns []string `json:"columns"`
	Rows    int64    `json:"rows"`
	Bytes   int64    `json:"bytes"`
	SHA256  string   `json:"sha256"`
}

// Entry is the table's archive entry name.
func (t Table) Entry() string { return dbPrefix + t.Name + ".tsv" }

// Skipped is a table that could not be read (damaged rows); the rest of the backup is intact.
type Skipped struct {
	Name  string `json:"name"`
	Error string `json:"error"`
}

// Sequence is a sequence value at snapshot time.
type Sequence struct {
	Name     string `json:"name"`
	Value    int64  `json:"value"`
	IsCalled bool   `json:"isCalled"`
}

// FileCounts says how many files of each kind the archive holds.
type FileCounts struct {
	Config  int `json:"config"`
	Avatars int `json:"avatars"`
	Images  int `json:"images"`
}

// SkippedNames lists the tables that are missing from the backup.
func (m *Manifest) SkippedNames() []string {
	out := make([]string, 0, len(m.Skipped))
	for _, s := range m.Skipped {
		out = append(out, s.Name)
	}
	return out
}

func (m *Manifest) check() error {
	if m.App != appName {
		return ErrInvalid
	}
	if m.Format < 1 || m.Format > FormatVersion {
		return fmt.Errorf("%w %d", errUnknownFormat, m.Format)
	}
	seen := map[string]bool{}
	for _, t := range m.Tables {
		if t.Name == "" || strings.ContainsAny(t.Name, "/\\\x00") || seen[t.Name] || len(t.Columns) == 0 {
			return fmt.Errorf("%w: bad table entry %q", ErrInvalid, t.Name)
		}
		seen[t.Name] = true
	}
	return nil
}

// reader walks an archive.
type reader struct {
	f  *os.File
	gz *gzip.Reader
	tr *tar.Reader
}

func open(p string) (*reader, *Manifest, error) {
	f, err := os.Open(p) //nolint:gosec // G304: callers pass a path resolved inside the backup folder or given on the CLI
	if err != nil {
		return nil, nil, err
	}
	gz, err := gzip.NewReader(f)
	if err != nil {
		_ = f.Close()
		return nil, nil, fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	r := &reader{f: f, gz: gz, tr: tar.NewReader(gz)}
	h, err := r.tr.Next()
	if err != nil || h.Name != manifestEntry || h.Size > maxManifestBytes {
		r.close()
		return nil, nil, ErrInvalid
	}
	var m Manifest
	if err := json.NewDecoder(io.LimitReader(r.tr, maxManifestBytes)).Decode(&m); err != nil {
		r.close()
		return nil, nil, fmt.Errorf("%w: manifest: %w", ErrInvalid, err)
	}
	if err := m.check(); err != nil {
		r.close()
		return nil, nil, err
	}
	return r, &m, nil
}

func (r *reader) close() {
	_ = r.gz.Close()
	_ = r.f.Close()
}

// finish reads to the end of the gzip stream so its CRC is verified.
func (r *reader) finish() error {
	if _, err := io.Copy(io.Discard, r.gz); err != nil {
		return fmt.Errorf("%w: %w", ErrInvalid, err)
	}
	return nil
}

// ReadManifest returns the manifest of an archive without reading the rest.
func ReadManifest(p string) (*Manifest, error) {
	r, m, err := open(p)
	if err != nil {
		return nil, err
	}
	r.close()
	return m, nil
}

// entryKind classifies an archive entry; it rejects anything this package does not write.
func entryKind(name string) (string, string, bool) {
	for _, prefix := range []string{dbPrefix, configPrefix, avatarPrefix, imagePrefix} {
		if rel, ok := strings.CutPrefix(name, prefix); ok {
			// path.Clean keeps the check on archive (slash) paths on every platform.
			if rel == "" || path.IsAbs(rel) || path.Clean(rel) != rel || rel == ".." || strings.HasPrefix(rel, "../") {
				return "", "", false
			}
			return prefix, rel, true
		}
	}
	return "", "", false
}

// Verify reads the whole archive: the manifest, every table against its checksum, the
// file entries' names, and the gzip checksum. Nothing is written.
func Verify(p string) (*Manifest, error) {
	r, m, err := open(p)
	if err != nil {
		return nil, err
	}
	defer r.close()
	tables := map[string]Table{}
	for _, t := range m.Tables {
		tables[t.Entry()] = t
	}
	seen := map[string]bool{}
	for {
		h, err := r.tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		prefix, _, ok := entryKind(h.Name)
		if !ok {
			return nil, fmt.Errorf("%w: unexpected entry %q", ErrInvalid, h.Name)
		}
		if prefix != dbPrefix {
			if _, err := io.Copy(io.Discard, r.tr); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
			}
			continue
		}
		t, ok := tables[h.Name]
		if !ok || seen[h.Name] {
			return nil, fmt.Errorf("%w: unexpected entry %q", ErrInvalid, h.Name)
		}
		seen[h.Name] = true
		sum := sha256.New()
		n, err := io.Copy(sum, r.tr)
		if err != nil {
			return nil, fmt.Errorf("%w: %w", ErrInvalid, err)
		}
		if n != t.Bytes || hex.EncodeToString(sum.Sum(nil)) != t.SHA256 {
			return nil, fmt.Errorf("%w: table %s is damaged", ErrInvalid, t.Name)
		}
	}
	if len(seen) != len(tables) {
		return nil, fmt.Errorf("%w: tables are missing", ErrInvalid)
	}
	if err := r.finish(); err != nil {
		return nil, err
	}
	return m, nil
}
