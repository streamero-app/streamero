package books

import (
	"encoding/binary"
	"errors"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

// MOBI / KF8 (AZW3) metadata, from the publicly documented PalmDB + MOBI header +
// EXTH layout. Only the header records are read; the text is never decompressed.

var errNotMOBI = errors.New("not a MOBI file")

const maxMobiRecord = 16 << 20

func readMOBI(p string, wantCover bool) (Info, []byte, error) {
	f, err := os.Open(p) //nolint:gosec // G304: validated by the caller
	if err != nil {
		return Info{}, nil, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return Info{}, nil, err
	}
	return ParseMOBI(f, st.Size(), wantCover)
}

// ParseMOBI reads title, authors, ISBN and friends from a MOBI/AZW3 file, and the cover image when asked.
func ParseMOBI(r io.ReaderAt, size int64, wantCover bool) (Info, []byte, error) {
	hdr := make([]byte, 78)
	if _, err := r.ReadAt(hdr, 0); err != nil {
		return Info{}, nil, errNotMOBI
	}
	if string(hdr[60:68]) != "BOOKMOBI" {
		return Info{}, nil, errNotMOBI
	}
	n := int(binary.BigEndian.Uint16(hdr[76:78]))
	if n < 1 || n > 65535 {
		return Info{}, nil, errNotMOBI
	}
	list := make([]byte, 8*n)
	if _, err := r.ReadAt(list, 78); err != nil {
		return Info{}, nil, errNotMOBI
	}
	offsets := make([]int64, n+1)
	for i := 0; i < n; i++ {
		offsets[i] = int64(binary.BigEndian.Uint32(list[8*i:]))
	}
	offsets[n] = size
	record := func(i int) ([]byte, error) {
		if i < 0 || i >= n || offsets[i] >= offsets[i+1] || offsets[i+1] > size || offsets[i+1]-offsets[i] > maxMobiRecord {
			return nil, errNotMOBI
		}
		b := make([]byte, offsets[i+1]-offsets[i])
		_, err := r.ReadAt(b, offsets[i])
		return b, err
	}
	r0, err := record(0)
	if err != nil || len(r0) < 132 || string(r0[16:20]) != "MOBI" {
		return Info{}, nil, errNotMOBI
	}
	be32 := func(b []byte, at int) int {
		if at+4 > len(b) {
			return 0
		}
		return int(binary.BigEndian.Uint32(b[at:]))
	}
	headerLen := be32(r0, 20)
	utf8Text := be32(r0, 28) == 65001
	text := func(b []byte) string {
		if utf8Text || utf8.Valid(b) {
			return clean(string(b))
		}
		rs := make([]rune, len(b))
		for i, c := range b {
			rs[i] = rune(c) // CP1252 ≈ Latin-1 for metadata
		}
		return clean(string(rs))
	}
	var in Info
	if off, l := be32(r0, 84), be32(r0, 88); off > 0 && l > 0 && off+l <= len(r0) {
		in.Title = text(r0[off : off+l])
	}
	firstImage := be32(r0, 108)
	coverOffset := -1
	if be32(r0, 128)&0x40 != 0 && 16+headerLen+12 <= len(r0) {
		exth := r0[16+headerLen:]
		if string(exth[:4]) == "EXTH" {
			count := be32(exth, 8)
			pos := 12
			for i := 0; i < count && pos+8 <= len(exth); i++ {
				typ, l := be32(exth, pos), be32(exth, pos+4)
				if l < 8 || pos+l > len(exth) {
					break
				}
				data := exth[pos+8 : pos+l]
				switch typ {
				case 100:
					for _, a := range strings.Split(text(data), ";") {
						if a = clean(a); a != "" {
							in.Authors = appendUnique(in.Authors, a)
						}
					}
				case 101:
					in.Publisher = text(data)
				case 103:
					in.Description = StripHTML(text(data))
				case 104:
					if isbn := NormalizeISBN(text(data)); isbn != "" {
						in.ISBN = isbn
					}
				case 105:
					in.Genres = appendUnique(in.Genres, text(data))
				case 106:
					in.Published = text(data)
					in.Year = yearOf(in.Published)
				case 201:
					if len(data) >= 4 {
						coverOffset = int(binary.BigEndian.Uint32(data))
					}
				case 503:
					if t := text(data); t != "" {
						in.Title = t
					}
				case 524:
					in.Language = text(data)
				}
				pos += l
			}
		}
	}
	if !wantCover || coverOffset < 0 || firstImage <= 0 || firstImage >= n {
		return in, nil, nil
	}
	cover, err := record(firstImage + coverOffset)
	if err != nil || len(cover) < 4 {
		return in, nil, nil //nolint:nilerr // a broken cover record is not a broken book
	}
	return in, cover, nil
}
