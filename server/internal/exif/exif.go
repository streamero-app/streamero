// Package exif reads the few EXIF fields a photo library needs (date taken,
// camera, lens, exposure, orientation, GPS) and picture dimensions from JPEG, PNG,
// WebP, HEIC and AVIF files. Written from the public EXIF/TIFF, PNG, WebP (RIFF)
// and ISO-BMFF (HEIF) specifications; nothing is decoded beyond the headers.
package exif

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"strconv"
	"strings"
	"time"
)

// Info is the photo metadata.
type Info struct {
	TakenAt      *time.Time `json:"takenAt,omitempty"`
	Width        int        `json:"width,omitempty"`
	Height       int        `json:"height,omitempty"`
	Make         string     `json:"make,omitempty"`
	Model        string     `json:"model,omitempty"`
	Lens         string     `json:"lens,omitempty"`
	FocalLength  float64    `json:"focalLength,omitempty"` // mm
	FNumber      float64    `json:"fNumber,omitempty"`
	ExposureTime string     `json:"exposureTime,omitempty"` // "1/250"
	ISO          int        `json:"iso,omitempty"`
	Orientation  int        `json:"orientation,omitempty"` // 1–8, EXIF meaning
	Latitude     *float64   `json:"latitude,omitempty"`
	Longitude    *float64   `json:"longitude,omitempty"`
}

// Camera is "Make Model" without the make repeated ("Canon Canon EOS R6" → "Canon EOS R6").
func (i Info) Camera() string {
	if i.Make == "" || strings.HasPrefix(strings.ToLower(i.Model), strings.ToLower(strings.Fields(i.Make)[0])) {
		return i.Model
	}
	return strings.TrimSpace(i.Make + " " + i.Model)
}

// ErrUnknownFormat is returned for files that are not a supported image.
var ErrUnknownFormat = errors.New("exif: unknown image format")

const (
	maxScan     = 16 << 20 // never read more than this looking for metadata
	maxExifSize = 1 << 20
)

// ReadFile reads a photo's metadata (path validated by the caller).
func ReadFile(p string) (Info, error) {
	f, err := os.Open(p) //nolint:gosec // G304: callers pass a path validated by library.ResolveSafePath
	if err != nil {
		return Info{}, err
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		return Info{}, err
	}
	return Read(f, st.Size())
}

// Read reads a photo's metadata from r.
func Read(r io.ReaderAt, size int64) (Info, error) {
	head := make([]byte, 32)
	n, _ := r.ReadAt(head, 0)
	head = head[:n]
	switch {
	case bytes.HasPrefix(head, []byte{0xFF, 0xD8}):
		return readJPEG(r, size)
	case bytes.HasPrefix(head, []byte("\x89PNG\r\n\x1a\n")):
		return readPNG(r, size)
	case len(head) >= 12 && string(head[0:4]) == "RIFF" && string(head[8:12]) == "WEBP":
		return readWebP(r, size)
	case len(head) >= 12 && string(head[4:8]) == "ftyp":
		return readHEIF(r, size)
	case bytes.HasPrefix(head, []byte("GIF8")) && len(head) >= 10:
		return Info{Width: int(binary.LittleEndian.Uint16(head[6:])), Height: int(binary.LittleEndian.Uint16(head[8:]))}, nil
	}
	return Info{}, ErrUnknownFormat
}

func readAt(r io.ReaderAt, off, n int64) ([]byte, error) {
	if n < 0 || n > maxExifSize*16 {
		return nil, errors.New("exif: block too large")
	}
	b := make([]byte, n)
	m, err := r.ReadAt(b, off)
	if int64(m) == n {
		return b, nil
	}
	if err == nil {
		err = io.ErrUnexpectedEOF
	}
	return nil, err
}

// --- JPEG ------------------------------------------------------------------------------

func readJPEG(r io.ReaderAt, size int64) (Info, error) {
	var in Info
	off := int64(2)
	for off+4 <= size && off < maxScan {
		h, err := readAt(r, off, 4)
		if err != nil {
			break
		}
		if h[0] != 0xFF {
			break
		}
		marker := h[1]
		if marker == 0xD8 || marker == 0x01 || (marker >= 0xD0 && marker <= 0xD7) {
			off += 2
			continue
		}
		if marker == 0xD9 || marker == 0xDA {
			break // image data starts: no metadata after this point
		}
		l := int64(binary.BigEndian.Uint16(h[2:]))
		if l < 2 {
			break
		}
		switch {
		case marker == 0xE1 && l > 8 && l-2 <= maxExifSize:
			seg, err := readAt(r, off+4, l-2)
			if err == nil && bytes.HasPrefix(seg, []byte("Exif\x00\x00")) {
				_ = parseTIFF(seg[6:], &in)
			}
		case (marker >= 0xC0 && marker <= 0xCF) && marker != 0xC4 && marker != 0xC8 && marker != 0xCC:
			if sof, err := readAt(r, off+4, 5); err == nil {
				in.Height = int(binary.BigEndian.Uint16(sof[1:]))
				in.Width = int(binary.BigEndian.Uint16(sof[3:]))
			}
		}
		off += 2 + l
	}
	return in, nil
}

// --- PNG -------------------------------------------------------------------------------

func readPNG(r io.ReaderAt, size int64) (Info, error) {
	var in Info
	off := int64(8)
	for off+8 <= size && off < maxScan {
		h, err := readAt(r, off, 8)
		if err != nil {
			break
		}
		l := int64(binary.BigEndian.Uint32(h))
		typ := string(h[4:8])
		switch typ {
		case "IHDR":
			if d, err := readAt(r, off+8, 8); err == nil {
				in.Width, in.Height = int(binary.BigEndian.Uint32(d)), int(binary.BigEndian.Uint32(d[4:]))
			}
		case "eXIf":
			if l <= maxExifSize {
				if d, err := readAt(r, off+8, l); err == nil {
					_ = parseTIFF(d, &in)
				}
			}
		case "IDAT", "IEND":
			return in, nil
		}
		off += 12 + l
	}
	return in, nil
}

// --- WebP ------------------------------------------------------------------------------

func readWebP(r io.ReaderAt, size int64) (Info, error) {
	var in Info
	off := int64(12)
	for off+8 <= size && off < maxScan {
		h, err := readAt(r, off, 8)
		if err != nil {
			break
		}
		l := int64(binary.LittleEndian.Uint32(h[4:]))
		switch string(h[:4]) {
		case "VP8X":
			if d, err := readAt(r, off+8, 10); err == nil {
				in.Width = 1 + int(d[4]) | int(d[5])<<8 | int(d[6])<<16
				in.Height = 1 + int(d[7]) | int(d[8])<<8 | int(d[9])<<16
			}
		case "VP8 ":
			if d, err := readAt(r, off+8, 10); err == nil && in.Width == 0 {
				in.Width = int(binary.LittleEndian.Uint16(d[6:]) & 0x3FFF)
				in.Height = int(binary.LittleEndian.Uint16(d[8:]) & 0x3FFF)
			}
		case "VP8L":
			if d, err := readAt(r, off+8, 5); err == nil && in.Width == 0 {
				bits := binary.LittleEndian.Uint32(d[1:])
				in.Width = int(bits&0x3FFF) + 1
				in.Height = int((bits>>14)&0x3FFF) + 1
			}
		case "EXIF":
			if l <= maxExifSize {
				if d, err := readAt(r, off+8, l); err == nil {
					_ = parseTIFF(bytes.TrimPrefix(d, []byte("Exif\x00\x00")), &in)
				}
			}
		}
		off += 8 + l + l%2
	}
	return in, nil
}

// --- HEIF / AVIF (ISO base media file format) -----------------------------------------

type box struct {
	typ        string
	start, end int64 // payload
}

func boxes(r io.ReaderAt, start, end int64) []box {
	var out []box
	for off := start; off+8 <= end && len(out) < 4096; {
		h, err := readAt(r, off, 8)
		if err != nil {
			break
		}
		size := int64(binary.BigEndian.Uint32(h))
		hdr := int64(8)
		switch size {
		case 1:
			ext, err := readAt(r, off+8, 8)
			if err != nil {
				return out
			}
			size = int64(binary.BigEndian.Uint64(ext)) //nolint:gosec // G115: checked against end below
			hdr = 16
		case 0:
			size = end - off
		}
		if size < hdr || off+size > end {
			break
		}
		out = append(out, box{typ: string(h[4:8]), start: off + hdr, end: off + size})
		off += size
	}
	return out
}

func find(bs []box, typ string) (box, bool) {
	for _, b := range bs {
		if b.typ == typ {
			return b, true
		}
	}
	return box{}, false
}

func readHEIF(r io.ReaderAt, size int64) (Info, error) {
	var in Info
	top := boxes(r, 0, min(size, maxScan))
	meta, ok := find(top, "meta")
	if !ok {
		return in, nil
	}
	children := boxes(r, meta.start+4, meta.end) // FullBox: version+flags
	// Picture size: the largest image spatial extent ("ispe") among the item properties.
	if iprp, ok := find(children, "iprp"); ok {
		if ipco, ok := find(boxes(r, iprp.start, iprp.end), "ipco"); ok {
			for _, p := range boxes(r, ipco.start, ipco.end) {
				if p.typ != "ispe" || p.end-p.start < 12 {
					continue
				}
				if d, err := readAt(r, p.start+4, 8); err == nil {
					w, h := int(binary.BigEndian.Uint32(d)), int(binary.BigEndian.Uint32(d[4:]))
					if w*h > in.Width*in.Height {
						in.Width, in.Height = w, h
					}
				}
			}
		}
	}
	// EXIF: the item of type "Exif" in "iinf", located through "iloc".
	exifID := -1
	if iinf, ok := find(children, "iinf"); ok {
		vh, err := readAt(r, iinf.start, 4)
		if err != nil {
			return in, nil //nolint:nilerr // no EXIF is fine
		}
		skip := int64(2)
		if vh[0] > 0 {
			skip = 4
		}
		for _, infe := range boxes(r, iinf.start+4+skip, iinf.end) {
			if infe.typ != "infe" {
				continue
			}
			d, err := readAt(r, infe.start, min(infe.end-infe.start, 64))
			if err != nil || len(d) < 12 || d[0] < 2 {
				continue
			}
			var id int
			var typ string
			if d[0] == 2 {
				id, typ = int(binary.BigEndian.Uint16(d[4:])), string(d[8:12])
			} else {
				if len(d) < 14 {
					continue
				}
				id, typ = int(binary.BigEndian.Uint32(d[4:])), string(d[10:14])
			}
			if typ == "Exif" {
				exifID = id
			}
		}
	}
	if exifID < 0 {
		return in, nil
	}
	iloc, ok := find(children, "iloc")
	if !ok {
		return in, nil
	}
	off, length, err := ilocExtent(r, iloc, exifID)
	if err != nil || length < 8 || length > maxExifSize {
		return in, nil //nolint:nilerr // no EXIF is fine
	}
	d, err := readAt(r, off, length)
	if err != nil {
		return in, nil //nolint:nilerr
	}
	// The payload starts with the offset of the TIFF header.
	skip := int(binary.BigEndian.Uint32(d))
	if 4+skip < len(d) {
		_ = parseTIFF(d[4+skip:], &in)
	}
	return in, nil
}

// ilocExtent returns the file offset and length of an item (first extent, construction method 0).
func ilocExtent(r io.ReaderAt, b box, id int) (int64, int64, error) {
	d, err := readAt(r, b.start, min(b.end-b.start, maxExifSize))
	if err != nil || len(d) < 8 {
		return 0, 0, errors.New("iloc")
	}
	version := d[0]
	offSize, lenSize := int(d[4]>>4), int(d[4]&15)
	baseSize, idxSize := int(d[5]>>4), 0
	if version == 1 || version == 2 {
		idxSize = int(d[5] & 15)
	}
	p := 6
	read := func(n int) (int64, bool) {
		if p+n > len(d) {
			return 0, false
		}
		var v uint64
		for i := 0; i < n; i++ {
			v = v<<8 | uint64(d[p+i])
		}
		p += n
		return int64(v), true //nolint:gosec // G115: at most 8 bytes, bounded by file size checks
	}
	var count int64
	var ok bool
	if version < 2 {
		count, ok = read(2)
	} else {
		count, ok = read(4)
	}
	if !ok || count > 10000 {
		return 0, 0, errors.New("iloc")
	}
	for i := int64(0); i < count; i++ {
		var itemID int64
		if version < 2 {
			itemID, ok = read(2)
		} else {
			itemID, ok = read(4)
		}
		if !ok {
			break
		}
		method := int64(0)
		if version == 1 || version == 2 {
			m, _ := read(2)
			method = m & 15
		}
		_, _ = read(2) // data reference index
		base, _ := read(baseSize)
		extents, ok := read(2)
		if !ok {
			break
		}
		var first [2]int64
		for e := int64(0); e < extents; e++ {
			_, _ = read(idxSize)
			eo, _ := read(offSize)
			el, ok := read(lenSize)
			if !ok {
				return 0, 0, errors.New("iloc")
			}
			if e == 0 {
				first = [2]int64{eo, el}
			}
		}
		if int(itemID) == id {
			if method != 0 {
				return 0, 0, errors.New("iloc: unsupported construction method")
			}
			return base + first[0], first[1], nil
		}
	}
	return 0, 0, errors.New("iloc: item not found")
}

// --- TIFF / EXIF IFDs -----------------------------------------------------------------

type tiff struct {
	b  []byte
	bo binary.ByteOrder
}

type entry struct {
	tag, typ uint16
	count    uint32
	value    []byte // the raw value bytes (inline or at the offset)
}

var typeSize = map[uint16]int{1: 1, 2: 1, 3: 2, 4: 4, 5: 8, 7: 1, 9: 4, 10: 8}

func (t tiff) ifd(off uint32) (map[uint16]entry, uint32) {
	out := map[uint16]entry{}
	if int(off)+2 > len(t.b) {
		return out, 0
	}
	n := int(t.bo.Uint16(t.b[off:]))
	if n > 1000 {
		return out, 0
	}
	p := int(off) + 2
	for i := 0; i < n && p+12 <= len(t.b); i, p = i+1, p+12 {
		e := entry{tag: t.bo.Uint16(t.b[p:]), typ: t.bo.Uint16(t.b[p+2:]), count: t.bo.Uint32(t.b[p+4:])}
		sz := typeSize[e.typ] * int(e.count)
		if sz <= 0 || e.count > 1<<20 {
			continue
		}
		if sz <= 4 {
			e.value = t.b[p+8 : p+8+sz]
		} else {
			vo := int(t.bo.Uint32(t.b[p+8:]))
			if vo < 0 || vo+sz > len(t.b) {
				continue
			}
			e.value = t.b[vo : vo+sz]
		}
		out[e.tag] = e
	}
	next := uint32(0)
	if p+4 <= len(t.b) {
		next = t.bo.Uint32(t.b[p:])
	}
	return out, next
}

func (t tiff) str(e entry) string {
	return strings.TrimSpace(strings.TrimRight(string(e.value), "\x00 "))
}

func (t tiff) uint(e entry) uint32 {
	switch e.typ {
	case 3:
		return uint32(t.bo.Uint16(e.value))
	case 4, 9:
		return t.bo.Uint32(e.value)
	case 1, 7:
		return uint32(e.value[0])
	}
	return 0
}

func (t tiff) rational(e entry, i int) float64 {
	if (e.typ != 5 && e.typ != 10) || len(e.value) < 8*(i+1) {
		return 0
	}
	num, den := t.bo.Uint32(e.value[8*i:]), t.bo.Uint32(e.value[8*i+4:])
	if den == 0 {
		return 0
	}
	if e.typ == 10 {
		return float64(int32(num)) / float64(int32(den)) //nolint:gosec // G115: signed rational by definition
	}
	return float64(num) / float64(den)
}

func parseTIFF(b []byte, in *Info) error {
	if len(b) < 8 {
		return errors.New("exif: short")
	}
	t := tiff{b: b}
	switch string(b[:2]) {
	case "II":
		t.bo = binary.LittleEndian
	case "MM":
		t.bo = binary.BigEndian
	default:
		return errors.New("exif: bad byte order")
	}
	ifd0, _ := t.ifd(t.bo.Uint32(b[4:]))
	if e, ok := ifd0[0x010F]; ok {
		in.Make = t.str(e)
	}
	if e, ok := ifd0[0x0110]; ok {
		in.Model = t.str(e)
	}
	if e, ok := ifd0[0x0112]; ok {
		if o := int(t.uint(e)); o >= 1 && o <= 8 {
			in.Orientation = o
		}
	}
	var dateTime string
	if e, ok := ifd0[0x0132]; ok {
		dateTime = t.str(e)
	}
	var original, offset string
	if e, ok := ifd0[0x8769]; ok {
		sub, _ := t.ifd(t.uint(e))
		if e, ok := sub[0x9003]; ok {
			original = t.str(e)
		}
		if e, ok := sub[0x9011]; ok {
			offset = t.str(e)
		}
		if e, ok := sub[0x829A]; ok {
			in.ExposureTime = exposure(t.rational(e, 0))
		}
		if e, ok := sub[0x829D]; ok {
			in.FNumber = round1(t.rational(e, 0))
		}
		if e, ok := sub[0x8827]; ok {
			in.ISO = int(t.uint(e))
		}
		if e, ok := sub[0x920A]; ok {
			in.FocalLength = round1(t.rational(e, 0))
		}
		if e, ok := sub[0xA434]; ok {
			in.Lens = t.str(e)
		}
		if in.Width == 0 {
			if e, ok := sub[0xA002]; ok {
				in.Width = int(t.uint(e))
			}
			if e, ok := sub[0xA003]; ok {
				in.Height = int(t.uint(e))
			}
		}
	}
	if original == "" {
		original = dateTime
	}
	if ts, ok := ParseDateTime(original, offset); ok {
		in.TakenAt = &ts
	}
	if e, ok := ifd0[0x8825]; ok {
		gps, _ := t.ifd(t.uint(e))
		lat, lok := gps[2]
		lon, gok := gps[4]
		if lok && gok {
			la := t.rational(lat, 0) + t.rational(lat, 1)/60 + t.rational(lat, 2)/3600
			lo := t.rational(lon, 0) + t.rational(lon, 1)/60 + t.rational(lon, 2)/3600
			if ref, ok := gps[1]; ok && strings.EqualFold(t.str(ref), "S") {
				la = -la
			}
			if ref, ok := gps[3]; ok && strings.EqualFold(t.str(ref), "W") {
				lo = -lo
			}
			if la != 0 || lo != 0 {
				if math.Abs(la) <= 90 && math.Abs(lo) <= 180 {
					la, lo = round6(la), round6(lo)
					in.Latitude, in.Longitude = &la, &lo
				}
			}
		}
	}
	return nil
}

// ParseDateTime reads EXIF "2006:01:02 15:04:05" with an optional "+02:00" offset.
// Without an offset the wall-clock time is stored as UTC (EXIF has no zone).
func ParseDateTime(s, offset string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if len(s) < 19 || strings.HasPrefix(s, "0000") {
		return time.Time{}, false
	}
	loc := time.UTC
	if len(offset) == 6 && (offset[0] == '+' || offset[0] == '-') {
		h, err1 := strconv.Atoi(offset[1:3])
		m, err2 := strconv.Atoi(offset[4:6])
		if err1 == nil && err2 == nil {
			sec := h*3600 + m*60
			if offset[0] == '-' {
				sec = -sec
			}
			loc = time.FixedZone(offset, sec)
		}
	}
	ts, err := time.ParseInLocation("2006:01:02 15:04:05", s[:19], loc)
	if err != nil || ts.Year() < 1826 || ts.Year() > 3000 {
		return time.Time{}, false
	}
	return ts.UTC(), true
}

func exposure(v float64) string {
	switch {
	case v <= 0:
		return ""
	case v >= 1:
		return strconv.FormatFloat(round1(v), 'f', -1, 64)
	}
	return fmt.Sprintf("1/%d", int(math.Round(1/v)))
}

func round1(v float64) float64 { return math.Round(v*10) / 10 }
func round6(v float64) float64 { return math.Round(v*1e6) / 1e6 }
