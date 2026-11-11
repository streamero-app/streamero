// Package livetv implements Live TV and the DVR (D-041): tuner sources (M3U
// playlists and HDHomeRun devices), XMLTV guides, channel ↔ guide mapping,
// live playback with time-shift, and recordings (single and series rules,
// padding, priorities, conflicts, post-processing and retention).
//
// Written from public descriptions only: the extended M3U format, the XMLTV
// DTD and the HDHomeRun HTTP API documentation.
package livetv

import (
	"bytes"
	"io"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"
	"golang.org/x/text/runes"
	xunicode "golang.org/x/text/encoding/unicode"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// decodeText turns playlist bytes into UTF-8: a UTF-8 or UTF-16 byte order mark
// is honoured, valid UTF-8 is kept, and anything else is read as Windows-1252
// (a superset of Latin-1, what most hand-made playlists use).
func decodeText(b []byte) string {
	switch {
	case bytes.HasPrefix(b, []byte{0xEF, 0xBB, 0xBF}):
		return string(b[3:])
	case bytes.HasPrefix(b, []byte{0xFF, 0xFE}), bytes.HasPrefix(b, []byte{0xFE, 0xFF}):
		out, _, err := transform.Bytes(xunicode.UTF16(xunicode.LittleEndian, xunicode.UseBOM).NewDecoder(), b)
		if err == nil {
			return string(out)
		}
	}
	if utf8.Valid(b) {
		return string(b)
	}
	out, _, err := transform.Bytes(charmap.Windows1252.NewDecoder(), b)
	if err != nil {
		return strings.ToValidUTF8(string(b), "�")
	}
	return string(out)
}

// readLimited reads at most limit bytes (an error past it: guides and
// playlists are not allowed to fill the memory).
func readLimited(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errTooLarge
	}
	return b, nil
}

// normName folds a channel name for matching: accents removed, lower case,
// only letters and digits, and common quality suffixes dropped ("HD", "FHD").
func normName(s string) string {
	t, _, err := transform.String(transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC), s)
	if err != nil {
		t = s
	}
	t = strings.ToLower(t)
	t = strings.NewReplacer("đ", "d", "ß", "ss", "æ", "ae", "ø", "o", "+", " plus ").Replace(t)
	fields := strings.FieldsFunc(t, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	for len(fields) > 1 {
		switch fields[len(fields)-1] {
		case "hd", "fhd", "uhd", "sd", "4k", "hevc", "h265", "1080p", "720p":
			fields = fields[:len(fields)-1]
			continue
		}
		break
	}
	return strings.Join(fields, "")
}
