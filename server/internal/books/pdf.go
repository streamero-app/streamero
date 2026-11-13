package books

import (
	"bytes"
	"compress/zlib"
	"io"
	"regexp"
	"strconv"
	"unicode/utf16"
)

// PDF reading is deliberately shallow: the page count and the document title and
// author are enough for a library, and a full PDF parser is a large attack surface.
// The page tree's root /Pages dictionary has the largest /Count; it is found in
// the plain file or inside compressed object streams (PDF 1.5+).

const (
	pdfChunk       = 1 << 20
	pdfOverlap     = 4 << 10
	maxObjStmBytes = 32 << 20
	maxObjStreams  = 2000
)

var (
	pagesTypeRe = regexp.MustCompile(`/Type\s*/Pages\b`)
	countRe     = regexp.MustCompile(`/Count\s+(\d{1,7})`)
	linearRe    = regexp.MustCompile(`/Linearized\s+[\d.]+[^>]*?/N\s+(\d{1,7})`)
	objStmRe    = regexp.MustCompile(`/Type\s*/ObjStm\b`)
	infoKeyRe   = regexp.MustCompile(`/(Title|Author)\s*(\(|<)`)
)

// ParsePDF returns the page count, title and author of a PDF.
func ParsePDF(r io.ReaderAt, size int64) (Info, error) {
	var in Info
	head := make([]byte, 8)
	if _, err := r.ReadAt(head, 0); err != nil || !bytes.HasPrefix(head, []byte("%PDF-")) {
		return in, ErrUnsupported
	}
	best, linear := 0, 0
	streams := 0
	buf := make([]byte, pdfChunk+pdfOverlap)
	for off := int64(0); off < size; off += pdfChunk {
		n, err := r.ReadAt(buf, off)
		if n == 0 && err != nil {
			break
		}
		chunk := buf[:n]
		best = max(best, maxPagesCount(chunk))
		if m := linearRe.FindSubmatch(chunk); m != nil && linear == 0 {
			linear, _ = strconv.Atoi(string(m[1]))
		}
		readInfo(chunk, &in)
		for _, loc := range objStmRe.FindAllIndex(chunk, -1) {
			if loc[0] >= pdfChunk || streams >= maxObjStreams {
				continue // the overlap belongs to the next chunk
			}
			streams++
			if data := objectStream(r, size, off+int64(loc[1])); data != nil {
				best = max(best, maxPagesCount(data))
				readInfo(data, &in)
			}
		}
	}
	in.PageCount = best
	if linear > 0 && best == 0 {
		in.PageCount = linear
	}
	return in, nil
}

// maxPagesCount finds the /Count closest to each /Type /Pages and returns the largest.
func maxPagesCount(b []byte) int {
	best := 0
	for _, loc := range pagesTypeRe.FindAllIndex(b, -1) {
		lo, hi := max(loc[0]-512, 0), min(loc[1]+512, len(b))
		win := b[lo:hi]
		closest, dist := 0, 1<<30
		for _, m := range countRe.FindAllSubmatchIndex(win, -1) {
			d := lo + m[0] - loc[0]
			if d < 0 {
				d = -d
			}
			// The count must belong to the same dictionary: no "obj" keyword in between.
			a, z := min(lo+m[0], loc[0]), max(lo+m[0], loc[0])
			if bytes.Contains(b[a:z], []byte(" obj")) || bytes.Contains(b[a:z], []byte("endobj")) {
				continue
			}
			if d < dist {
				n, _ := strconv.Atoi(string(win[m[2]:m[3]]))
				closest, dist = n, d
			}
		}
		best = max(best, closest)
	}
	return best
}

// objectStream inflates the stream that follows an /ObjStm dictionary.
func objectStream(r io.ReaderAt, size, from int64) []byte {
	look := make([]byte, 4096)
	n, _ := r.ReadAt(look, from)
	i := bytes.Index(look[:n], []byte("stream"))
	if i < 0 || !bytes.Contains(look[:i], []byte("/FlateDecode")) {
		return nil
	}
	start := from + int64(i) + int64(len("stream"))
	// Skip the end-of-line after the keyword.
	eol := make([]byte, 2)
	if _, err := r.ReadAt(eol, start); err == nil {
		switch eol[0] {
		case '\r':
			start++
			if eol[1] == '\n' {
				start++
			}
		case '\n':
			start++
		}
	}
	zr, err := zlib.NewReader(io.NewSectionReader(r, start, size-start))
	if err != nil {
		return nil
	}
	defer func() { _ = zr.Close() }()
	data, _ := io.ReadAll(io.LimitReader(zr, maxObjStmBytes))
	return data
}

// readInfo picks /Title and /Author from a document information dictionary
// (recognised by /Producer, /Creator or /CreationDate next to them).
func readInfo(b []byte, in *Info) {
	if in.Title != "" && len(in.Authors) > 0 {
		return
	}
	for _, m := range infoKeyRe.FindAllSubmatchIndex(b, -1) {
		lo, hi := max(m[0]-1024, 0), min(m[1]+1024, len(b))
		win := b[lo:hi]
		if !bytes.Contains(win, []byte("/Producer")) && !bytes.Contains(win, []byte("/CreationDate")) && !bytes.Contains(win, []byte("/Creator")) {
			continue
		}
		val := pdfString(b[m[4]:])
		if val == "" {
			continue
		}
		switch string(b[m[2]:m[3]]) {
		case "Title":
			if in.Title == "" {
				in.Title = val
			}
		case "Author":
			if len(in.Authors) == 0 {
				in.Authors = splitList(val)
			}
		}
	}
}

// pdfString decodes a literal "(…)" or hex "<…>" string at the start of b.
func pdfString(b []byte) string {
	var raw []byte
	switch {
	case len(b) > 0 && b[0] == '(':
		depth := 0
		for i := 0; i < len(b) && i < 4096; i++ {
			c := b[i]
			switch {
			case c == '\\' && i+1 < len(b):
				i++
				switch e := b[i]; e {
				case 'n':
					raw = append(raw, '\n')
				case 'r', 't', 'b', 'f':
					raw = append(raw, ' ')
				case '0', '1', '2', '3', '4', '5', '6', '7':
					v := 0
					j := 0
					for ; j < 3 && i+j < len(b) && b[i+j] >= '0' && b[i+j] <= '7'; j++ {
						v = v*8 + int(b[i+j]-'0')
					}
					i += j - 1
					raw = append(raw, byte(v)) //nolint:gosec // G115: octal escape is at most 0o777
				case '\n', '\r':
				default:
					raw = append(raw, e)
				}
				continue
			case c == '(':
				depth++
				if depth == 1 {
					continue
				}
			case c == ')':
				depth--
				if depth == 0 {
					return decodePDFText(raw)
				}
			}
			raw = append(raw, c)
		}
		return ""
	case len(b) > 0 && b[0] == '<':
		end := bytes.IndexByte(b, '>')
		if end < 0 || end > 8192 {
			return ""
		}
		hex := bytes.Join(bytes.Fields(b[1:end]), nil)
		if len(hex)%2 == 1 {
			hex = append(hex, '0')
		}
		for i := 0; i+1 < len(hex); i += 2 {
			v, err := strconv.ParseUint(string(hex[i:i+2]), 16, 8)
			if err != nil {
				return ""
			}
			raw = append(raw, byte(v))
		}
		return decodePDFText(raw)
	}
	return ""
}

// decodePDFText handles UTF-16BE text strings (with BOM) and PDFDocEncoding (≈ Latin-1).
func decodePDFText(raw []byte) string {
	if len(raw) >= 2 && raw[0] == 0xFE && raw[1] == 0xFF {
		u := make([]uint16, 0, len(raw)/2)
		for i := 2; i+1 < len(raw); i += 2 {
			u = append(u, uint16(raw[i])<<8|uint16(raw[i+1]))
		}
		return clean(string(utf16.Decode(u)))
	}
	if len(raw) >= 3 && raw[0] == 0xEF && raw[1] == 0xBB && raw[2] == 0xBF {
		return clean(string(raw[3:]))
	}
	r := make([]rune, len(raw))
	for i, c := range raw {
		r[i] = rune(c)
	}
	return clean(string(r))
}
