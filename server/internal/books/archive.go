// Package books reads e-books, comic archives and their metadata straight from
// the files (EPUB/OPF, ComicInfo.xml, PDF, MOBI/AZW3) without extracting
// anything to disk. Everything here follows public specifications only (D-039).
package books

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"unicode"

	"github.com/bodgit/sevenzip"
	"github.com/nwaples/rardecode/v2"
)

// Limits that keep a hostile archive (zip bomb, millions of entries) from
// exhausting memory or CPU. Nothing is ever written to disk.
const (
	// MaxEntries is the most entries an archive may list.
	MaxEntries = 20000
	// MaxEntryBytes is the largest single entry (a page or metadata file) that is read.
	MaxEntryBytes = 64 << 20
	// MaxMetaBytes caps XML metadata files (ComicInfo.xml, OPF, container.xml).
	MaxMetaBytes = 4 << 20
	// MaxRatio is the largest allowed uncompressed/compressed ratio for entries over 1 MiB.
	MaxRatio = 200
	// maxRarDictionary limits the memory a RAR decoder may allocate.
	maxRarDictionary = 256 << 20
)

// Errors returned for unsafe or unreadable archives.
var (
	ErrTooManyEntries = errors.New("archive has too many entries")
	ErrEntryTooLarge  = errors.New("archive entry is too large")
	ErrSuspiciousRate = errors.New("archive entry compresses suspiciously well (zip bomb?)")
	ErrUnsafeName     = errors.New("archive entry has an unsafe name")
	ErrNoSuchPage     = errors.New("no such page")
	ErrUnsupported    = errors.New("unsupported archive format")
)

// Entry is a file inside an archive.
type Entry struct {
	Name           string
	Size           int64 // uncompressed size as declared by the archive (-1 when unknown)
	CompressedSize int64 // -1 when unknown
}

// Archive gives read access to a comic archive or EPUB container.
type Archive interface {
	// Entries lists the regular files with safe names.
	Entries() []Entry
	// ReadFile returns one entry's bytes, read through the size guards.
	ReadFile(name string, limit int64) ([]byte, error)
	Close() error
}

// SafeName reports whether an entry name stays inside the archive: no absolute
// paths, no "..", no drive letters, no NUL bytes. Nothing is extracted, but names
// are still checked so a hostile entry can never be mistaken for a page or path.
func SafeName(name string) bool {
	if name == "" || strings.ContainsRune(name, 0) || strings.HasPrefix(name, "/") || strings.HasPrefix(name, "\\") {
		return false
	}
	if len(name) > 1 && name[1] == ':' {
		return false
	}
	for _, part := range strings.FieldsFunc(name, func(r rune) bool { return r == '/' || r == '\\' }) {
		if part == ".." {
			return false
		}
	}
	return true
}

// checkEntry applies the size and ratio guards to a declared entry.
func checkEntry(e Entry, limit int64) error {
	if e.Size > limit {
		return ErrEntryTooLarge
	}
	if e.Size > 1<<20 && e.CompressedSize > 0 && e.Size/e.CompressedSize > MaxRatio {
		return ErrSuspiciousRate
	}
	return nil
}

// readLimited reads at most limit bytes and fails if there is more (the declared
// size of an entry can lie).
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	var buf bytes.Buffer
	n, err := io.Copy(&buf, io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if n > limit {
		return nil, ErrEntryTooLarge
	}
	return buf.Bytes(), nil
}

// ArchiveFormat names the container of a comic file from its extension and,
// when the extension lies (a CBR that is really a ZIP is common), its magic bytes.
func ArchiveFormat(p string) string {
	f, err := os.Open(p) //nolint:gosec // G304: callers pass a path validated by library.ResolveSafePath
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	head := make([]byte, 262)
	n, _ := io.ReadFull(f, head)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte("PK\x03\x04")) || bytes.HasPrefix(head, []byte("PK\x05\x06")):
		return "zip"
	case bytes.HasPrefix(head, []byte("Rar!\x1a\x07")):
		return "rar"
	case bytes.HasPrefix(head, []byte{'7', 'z', 0xBC, 0xAF, 0x27, 0x1C}):
		return "7z"
	case len(head) >= 262 && string(head[257:262]) == "ustar":
		return "tar"
	}
	switch strings.ToLower(filepath.Ext(p)) {
	case ".cbt", ".tar":
		return "tar" // old-style tar without the ustar magic
	}
	return ""
}

// OpenArchive opens a ZIP (CBZ, EPUB), RAR (CBR), 7z (CB7) or TAR (CBT) archive.
func OpenArchive(p string) (Archive, error) {
	switch ArchiveFormat(p) {
	case "zip":
		return openZip(p)
	case "rar":
		return openRar(p)
	case "7z":
		return open7z(p)
	case "tar":
		return openTar(p)
	}
	return nil, ErrUnsupported
}

// --- zip ------------------------------------------------------------------------------

type zipArchive struct {
	rc      *zip.ReadCloser
	files   map[string]*zip.File
	entries []Entry
}

func openZip(p string) (Archive, error) {
	rc, err := zip.OpenReader(p)
	if errors.Is(err, zip.ErrInsecurePath) && rc != nil {
		err = nil // the reader is valid; unsafe names are skipped below
	}
	if err != nil {
		return nil, err
	}
	if len(rc.File) > MaxEntries {
		_ = rc.Close()
		return nil, ErrTooManyEntries
	}
	z := &zipArchive{rc: rc, files: map[string]*zip.File{}}
	for _, f := range rc.File {
		if f.FileInfo().IsDir() || !SafeName(f.Name) {
			continue
		}
		z.files[f.Name] = f
		z.entries = append(z.entries, Entry{Name: f.Name, Size: int64(f.UncompressedSize64), CompressedSize: int64(f.CompressedSize64)}) //nolint:gosec // G115: sizes fit int64
	}
	return z, nil
}

func (z *zipArchive) Entries() []Entry { return z.entries }
func (z *zipArchive) Close() error     { return z.rc.Close() }

func (z *zipArchive) ReadFile(name string, limit int64) ([]byte, error) {
	f, ok := z.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	if err := checkEntry(Entry{Size: int64(f.UncompressedSize64), CompressedSize: int64(f.CompressedSize64)}, limit); err != nil { //nolint:gosec // G115
		return nil, err
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return readLimited(r, limit)
}

// --- tar ------------------------------------------------------------------------------

type tarArchive struct {
	f       *os.File
	entries []Entry
}

func openTar(p string) (Archive, error) {
	f, err := os.Open(p) //nolint:gosec // G304: validated by the caller
	if err != nil {
		return nil, err
	}
	t := &tarArchive{f: f}
	tr := tar.NewReader(f)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			_ = f.Close()
			return nil, err
		}
		if len(t.entries) >= MaxEntries {
			_ = f.Close()
			return nil, ErrTooManyEntries
		}
		if h.Typeflag != tar.TypeReg || !SafeName(h.Name) {
			continue // links and devices are never followed
		}
		t.entries = append(t.entries, Entry{Name: h.Name, Size: h.Size, CompressedSize: -1})
	}
	return t, nil
}

func (t *tarArchive) Entries() []Entry { return t.entries }
func (t *tarArchive) Close() error     { return t.f.Close() }

func (t *tarArchive) ReadFile(name string, limit int64) ([]byte, error) {
	if _, err := t.f.Seek(0, io.SeekStart); err != nil {
		return nil, err
	}
	tr := tar.NewReader(t.f)
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			return nil, os.ErrNotExist
		}
		if err != nil {
			return nil, err
		}
		if h.Typeflag == tar.TypeReg && h.Name == name {
			if h.Size > limit {
				return nil, ErrEntryTooLarge
			}
			return readLimited(tr, limit)
		}
	}
}

// --- rar ------------------------------------------------------------------------------

type rarArchive struct {
	path    string
	entries []Entry
}

func openRar(p string) (Archive, error) {
	a := &rarArchive{path: p}
	err := a.walk(func(h *rardecode.FileHeader, _ io.Reader) (bool, error) {
		if len(a.entries) >= MaxEntries {
			return true, ErrTooManyEntries
		}
		if !h.IsDir && !h.Encrypted && h.LinkType == 0 && SafeName(h.Name) {
			size := h.UnPackedSize
			if h.UnKnownSize {
				size = -1
			}
			a.entries = append(a.entries, Entry{Name: h.Name, Size: size, CompressedSize: h.PackedSize})
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return a, nil
}

// walk visits the archive's entries in order (RAR is read sequentially).
func (a *rarArchive) walk(fn func(h *rardecode.FileHeader, r io.Reader) (stop bool, err error)) error {
	f, err := os.Open(a.path) //nolint:gosec // G304: validated by the caller
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	r, err := rardecode.NewReader(f, rardecode.MaxDictionarySize(maxRarDictionary))
	if err != nil {
		return err
	}
	for {
		h, err := r.Next()
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		stop, err := fn(h, r)
		if err != nil || stop {
			return err
		}
	}
}

func (a *rarArchive) Entries() []Entry { return a.entries }
func (a *rarArchive) Close() error     { return nil }

func (a *rarArchive) ReadFile(name string, limit int64) ([]byte, error) {
	var out []byte
	found := false
	err := a.walk(func(h *rardecode.FileHeader, r io.Reader) (bool, error) {
		if h.Name != name || h.IsDir {
			return false, nil
		}
		found = true
		if err := checkEntry(Entry{Size: h.UnPackedSize, CompressedSize: h.PackedSize}, limit); err != nil {
			return true, err
		}
		b, err := readLimited(r, limit)
		out = b
		return true, err
	})
	if err == nil && !found {
		err = os.ErrNotExist
	}
	return out, err
}

// --- 7z -------------------------------------------------------------------------------

type sevenZipArchive struct {
	rc      *sevenzip.ReadCloser
	files   map[string]*sevenzip.File
	entries []Entry
}

func open7z(p string) (a Archive, err error) {
	defer func() {
		// A corrupt header must not take the server down.
		if r := recover(); r != nil {
			a, err = nil, fmt.Errorf("7z: %v", r)
		}
	}()
	rc, err := sevenzip.OpenReader(p)
	if err != nil {
		return nil, err
	}
	if len(rc.File) > MaxEntries {
		_ = rc.Close()
		return nil, ErrTooManyEntries
	}
	s := &sevenZipArchive{rc: rc, files: map[string]*sevenzip.File{}}
	for _, f := range rc.File {
		if f.FileInfo().IsDir() || !f.FileInfo().Mode().IsRegular() || !SafeName(f.Name) {
			continue
		}
		s.files[f.Name] = f
		s.entries = append(s.entries, Entry{Name: f.Name, Size: int64(f.UncompressedSize), CompressedSize: -1}) //nolint:gosec // G115
	}
	return s, nil
}

func (s *sevenZipArchive) Entries() []Entry { return s.entries }
func (s *sevenZipArchive) Close() error     { return s.rc.Close() }

func (s *sevenZipArchive) ReadFile(name string, limit int64) ([]byte, error) {
	f, ok := s.files[name]
	if !ok {
		return nil, os.ErrNotExist
	}
	if int64(f.UncompressedSize) > limit { //nolint:gosec // G115
		return nil, ErrEntryTooLarge
	}
	r, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer func() { _ = r.Close() }()
	return readLimited(r, limit)
}

// --- pages ----------------------------------------------------------------------------

var pageExts = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp", ".avif": "image/avif", ".bmp": "image/bmp"}

// PageType returns the MIME type of a page image name ("" if it is not an image).
func PageType(name string) string { return pageExts[strings.ToLower(path.Ext(name))] }

// Pages returns the image entries of a comic archive in reading order: natural
// sort of the full names, skipping macOS resource forks and hidden files.
func Pages(entries []Entry) []string {
	var out []string
	for _, e := range entries {
		base := path.Base(strings.ReplaceAll(e.Name, "\\", "/"))
		if strings.HasPrefix(base, ".") || strings.Contains(e.Name, "__MACOSX") || PageType(e.Name) == "" {
			continue
		}
		out = append(out, e.Name)
	}
	sort.SliceStable(out, func(i, j int) bool { return NaturalLess(out[i], out[j]) })
	return out
}

// NaturalLess compares strings with digit runs by number ("page2" < "page10"), case-insensitively.
func NaturalLess(a, b string) bool {
	ra, rb := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si := i
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			sj := j
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na := strings.TrimLeft(string(ra[si:i]), "0")
			nb := strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		if ra[i] != rb[j] {
			return ra[i] < rb[j]
		}
		i++
		j++
	}
	return len(ra)-i < len(rb)-j
}

// ReadPage returns page n (0-based) of a comic archive and its MIME type.
func ReadPage(a Archive, n int) ([]byte, string, error) {
	pages := Pages(a.Entries())
	if n < 0 || n >= len(pages) {
		return nil, "", ErrNoSuchPage
	}
	data, err := a.ReadFile(pages[n], MaxEntryBytes)
	return data, PageType(pages[n]), err
}
