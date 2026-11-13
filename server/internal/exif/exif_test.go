package exif

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/jpeg"
	"image/png"
	"testing"
	"time"
)

// tiffBuilder writes a little-endian TIFF block with IFD0, an EXIF IFD and a GPS IFD.
type tag struct {
	id, typ uint16
	count   uint32
	data    []byte
}

func ascii(id uint16, s string) tag {
	return tag{id, 2, uint32(len(s) + 1), append([]byte(s), 0)}
}

func short(id uint16, v uint16) tag {
	b := make([]byte, 2)
	binary.LittleEndian.PutUint16(b, v)
	return tag{id, 3, 1, b}
}

func long(id uint16, v uint32) tag {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return tag{id, 4, 1, b}
}

func rationals(id uint16, vals ...[2]uint32) tag {
	b := make([]byte, 8*len(vals))
	for i, v := range vals {
		binary.LittleEndian.PutUint32(b[8*i:], v[0])
		binary.LittleEndian.PutUint32(b[8*i+4:], v[1])
	}
	return tag{id, 5, uint32(len(vals)), b}
}

// buildTIFF lays out IFDs one after another; pointer tags (0x8769, 0x8825) are patched.
func buildTIFF(ifd0 []tag, exifIFD []tag, gps []tag) []byte {
	out := []byte{'I', 'I', 42, 0, 8, 0, 0, 0}
	write := func(tags []tag) (start uint32, patch map[uint16]int) {
		patch = map[uint16]int{}
		start = uint32(len(out))
		dataOff := len(out) + 2 + 12*len(tags) + 4
		var data []byte
		hdr := make([]byte, 2)
		binary.LittleEndian.PutUint16(hdr, uint16(len(tags)))
		out = append(out, hdr...)
		for _, t := range tags {
			e := make([]byte, 12)
			binary.LittleEndian.PutUint16(e, t.id)
			binary.LittleEndian.PutUint16(e[2:], t.typ)
			binary.LittleEndian.PutUint32(e[4:], t.count)
			if len(t.data) <= 4 {
				copy(e[8:], t.data)
			} else {
				binary.LittleEndian.PutUint32(e[8:], uint32(dataOff+len(data)))
				data = append(data, t.data...)
			}
			patch[t.id] = len(out) + 8
			out = append(out, e...)
		}
		out = append(out, 0, 0, 0, 0)
		out = append(out, data...)
		return start, patch
	}
	_, p0 := write(ifd0)
	if exifIFD != nil {
		s, _ := write(exifIFD)
		binary.LittleEndian.PutUint32(out[p0[0x8769]:], s)
	}
	if gps != nil {
		s, _ := write(gps)
		binary.LittleEndian.PutUint32(out[p0[0x8825]:], s)
	}
	return out
}

func sampleTIFF() []byte {
	return buildTIFF(
		[]tag{ascii(0x010F, "Canon"), ascii(0x0110, "Canon EOS R6"), short(0x0112, 6), long(0x8769, 0), long(0x8825, 0)},
		[]tag{ascii(0x9003, "2023:07:14 18:30:05"), ascii(0x9011, "+02:00"), rationals(0x829A, [2]uint32{1, 250}),
			rationals(0x829D, [2]uint32{28, 10}), short(0x8827, 400), rationals(0x920A, [2]uint32{50, 1}), ascii(0xA434, "RF50mm F1.8 STM")},
		[]tag{ascii(1, "N"), rationals(2, [2]uint32{45, 1}, [2]uint32{48, 1}, [2]uint32{36, 1}),
			ascii(3, "W"), rationals(4, [2]uint32{15, 1}, [2]uint32{58, 1}, [2]uint32{12, 1})},
	)
}

func checkSample(t *testing.T, in Info) {
	t.Helper()
	want := time.Date(2023, 7, 14, 16, 30, 5, 0, time.UTC)
	if in.TakenAt == nil || !in.TakenAt.Equal(want) {
		t.Fatalf("taken %v", in.TakenAt)
	}
	if in.Camera() != "Canon EOS R6" || in.Lens != "RF50mm F1.8 STM" || in.ExposureTime != "1/250" || in.FNumber != 2.8 ||
		in.ISO != 400 || in.FocalLength != 50 || in.Orientation != 6 {
		t.Fatalf("%+v", in)
	}
	if in.Latitude == nil || *in.Latitude != 45.81 || in.Longitude == nil || *in.Longitude != -15.97 {
		t.Fatalf("gps %v %v", in.Latitude, in.Longitude)
	}
}

func encodeJPEG(t *testing.T, w, h int) []byte {
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, image.NewRGBA(image.Rect(0, 0, w, h)), nil); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// WithEXIF inserts an APP1 EXIF segment after the JPEG SOI marker.
func WithEXIF(jpg, tiffData []byte) []byte {
	seg := append([]byte("Exif\x00\x00"), tiffData...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func TestJPEG(t *testing.T) {
	data := WithEXIF(encodeJPEG(t, 64, 48), sampleTIFF())
	in, err := Read(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	checkSample(t, in)
	if in.Width != 64 || in.Height != 48 {
		t.Fatalf("size %dx%d", in.Width, in.Height)
	}
}

func TestJPEGWithoutEXIF(t *testing.T) {
	data := encodeJPEG(t, 10, 20)
	in, err := Read(bytes.NewReader(data), int64(len(data)))
	if err != nil || in.Width != 10 || in.Height != 20 || in.TakenAt != nil {
		t.Fatalf("%+v %v", in, err)
	}
}

func chunk(typ string, data []byte) []byte {
	out := make([]byte, 8)
	binary.BigEndian.PutUint32(out, uint32(len(data)))
	copy(out[4:], typ)
	out = append(out, data...)
	return append(out, 0, 0, 0, 0) // CRC is not checked
}

func TestPNG(t *testing.T) {
	var buf bytes.Buffer
	_ = png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 30, 40)))
	b := buf.Bytes()
	// Insert eXIf right after IHDR (8 signature + 25 IHDR).
	data := append(append(append([]byte{}, b[:33]...), chunk("eXIf", sampleTIFF())...), b[33:]...)
	in, err := Read(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	checkSample(t, in)
	if in.Width != 30 || in.Height != 40 {
		t.Fatalf("size %dx%d", in.Width, in.Height)
	}
}

func TestWebP(t *testing.T) {
	riff := func(typ string, data []byte) []byte {
		out := make([]byte, 8)
		copy(out, typ)
		binary.LittleEndian.PutUint32(out[4:], uint32(len(data)))
		out = append(out, data...)
		if len(data)%2 == 1 {
			out = append(out, 0)
		}
		return out
	}
	vp8x := make([]byte, 10)
	vp8x[0] = 0x08                // EXIF flag
	vp8x[4], vp8x[5] = 0xFF, 0x0E // width-1 = 3839
	vp8x[7], vp8x[8] = 0x6F, 0x08 // height-1 = 2159
	body := append([]byte("WEBP"), riff("VP8X", vp8x)...)
	body = append(body, riff("EXIF", append([]byte("Exif\x00\x00"), sampleTIFF()...))...)
	data := append([]byte("RIFF"), make([]byte, 4)...)
	binary.LittleEndian.PutUint32(data[4:], uint32(len(body)))
	data = append(data, body...)
	in, err := Read(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	checkSample(t, in)
	if in.Width != 3840 || in.Height != 2160 {
		t.Fatalf("size %dx%d", in.Width, in.Height)
	}
}

// bmff builds an ISO-BMFF box.
func bmff(typ string, payload ...[]byte) []byte {
	var body []byte
	for _, p := range payload {
		body = append(body, p...)
	}
	out := make([]byte, 8)
	binary.BigEndian.PutUint32(out, uint32(8+len(body)))
	copy(out[4:], typ)
	return append(out, body...)
}

func TestHEIF(t *testing.T) {
	exifPayload := append([]byte{0, 0, 0, 6}, append([]byte("Exif\x00\x00"), sampleTIFF()...)...)
	infe := func(id uint16, typ string) []byte {
		b := []byte{2, 0, 0, 0, 0, 0, 0, 0}
		binary.BigEndian.PutUint16(b[4:], id)
		b = append(b, typ...)
		return bmff("infe", append(b, 0)) // empty item name
	}
	iinf := bmff("iinf", []byte{0, 0, 0, 0, 0, 2}, infe(1, "hvc1"), infe(2, "Exif"))
	ispe := func(w, h uint32) []byte {
		b := make([]byte, 12)
		binary.BigEndian.PutUint32(b[4:], w)
		binary.BigEndian.PutUint32(b[8:], h)
		return bmff("ispe", b)
	}
	iprp := bmff("iprp", bmff("ipco", ispe(512, 512), ispe(4032, 3024)))
	ftyp := bmff("ftyp", []byte("heic\x00\x00\x00\x00mif1heic"))
	// iloc v0: offset_size 4, length_size 4, base_offset_size 0; offsets patched below.
	iloc := func(off uint32) []byte {
		b := []byte{0, 0, 0, 0, 0x44, 0x00, 0, 1}
		item := make([]byte, 2+2+2+4+4)
		binary.BigEndian.PutUint16(item, 2)
		binary.BigEndian.PutUint16(item[4:], 1)
		binary.BigEndian.PutUint32(item[6:], off)
		binary.BigEndian.PutUint32(item[10:], uint32(len(exifPayload)))
		return bmff("iloc", append(b, item...))
	}
	build := func(off uint32) []byte {
		meta := bmff("meta", []byte{0, 0, 0, 0}, bmff("hdlr", make([]byte, 24)), iinf, iprp, iloc(off))
		return append(append(ftyp, meta...), bmff("mdat", exifPayload)...)
	}
	probe := build(0)
	data := build(uint32(len(probe) - len(exifPayload)))
	in, err := Read(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	checkSample(t, in)
	if in.Width != 4032 || in.Height != 3024 {
		t.Fatalf("size %dx%d", in.Width, in.Height)
	}
}

func TestGarbageDoesNotPanic(_ *testing.T) {
	inputs := [][]byte{
		{0xFF, 0xD8, 0xFF, 0xE1, 0xFF, 0xFF, 'E', 'x', 'i', 'f', 0, 0, 'I', 'I', 42, 0, 0xFF, 0xFF, 0xFF, 0x7F},
		append([]byte("\x00\x00\x00\x10ftypheic"), bmff("meta", []byte{0, 0, 0, 0}, bmff("iloc", []byte{2, 0, 0, 0, 0xFF, 0xFF}))...),
		[]byte("RIFF\xff\xff\xff\xffWEBPEXIF\xff\xff\xff\x7f"),
		[]byte("not an image"),
	}
	for _, d := range inputs {
		_, _ = Read(bytes.NewReader(d), int64(len(d)))
	}
	var in Info
	_ = parseTIFF([]byte{'M', 'M', 0, 42, 0, 0, 0, 8, 0xFF, 0xFF}, &in)
}

func TestParseDateTime(t *testing.T) {
	if _, ok := ParseDateTime("0000:00:00 00:00:00", ""); ok {
		t.Fatal("zero date")
	}
	ts, ok := ParseDateTime("2020:01:02 03:04:05", "")
	if !ok || !ts.Equal(time.Date(2020, 1, 2, 3, 4, 5, 0, time.UTC)) {
		t.Fatal(ts)
	}
}
