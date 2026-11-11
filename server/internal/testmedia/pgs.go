// Package testmedia writes small media files for tests that ffmpeg cannot make
// itself (ffmpeg has no PGS encoder).
package testmedia

import "encoding/binary"

// Box is a rectangle in a subtitle canvas.
type Box struct{ X, Y, W, H int }

func be16(v int) []byte { return []byte{byte(v >> 8), byte(v)} } // #nosec G115 -- bytes of a 16-bit field

// segment is one segment of a Blu-ray PGS (.sup) stream.
func segment(kind byte, pts uint32, data []byte) []byte {
	b := []byte{'P', 'G', 0, 0, 0, 0, 0, 0, 0, 0, kind, 0, 0}
	binary.BigEndian.PutUint32(b[2:], pts)
	binary.BigEndian.PutUint16(b[11:], uint16(len(data))) // #nosec G115 -- small test data
	return append(b, data...)
}

// PGSBox returns a .sup stream for a width×height canvas that shows a solid
// white box from showMS to hideMS.
func PGSBox(width, height int, box Box, showMS, hideMS int64) []byte {
	show, hide := uint32(showMS*90), uint32(hideMS*90) // #nosec G115 -- 90 kHz clock, test times
	window := append(append(append(append([]byte{1, 0}, be16(box.X)...), be16(box.Y)...), be16(box.W)...), be16(box.H)...)
	pcs := func(number int, state byte, object bool) []byte {
		b := append(append(be16(width), be16(height)...), 0x10)
		b = append(append(b, be16(number)...), state, 0, 0)
		if !object {
			return append(b, 0)
		}
		b = append(append(append(b, 1), be16(0)...), 0, 0)
		return append(append(b, be16(box.X)...), be16(box.Y)...)
	}
	// Each line: box.W pixels of palette entry 1, then end of line.
	var rle []byte
	for range box.H {
		rle = append(rle, 0, 0xC0|byte(box.W>>8&0x3F), byte(box.W), 1, 0, 0) // #nosec G115 -- 14-bit run length
	}
	n := len(rle) + 4
	ods := append(be16(0), 0, 0xC0, byte(n>>16), byte(n>>8), byte(n)) // #nosec G115 -- 24-bit length
	ods = append(append(append(ods, be16(box.W)...), be16(box.H)...), rle...)
	var sup []byte
	sup = append(sup, segment(0x16, show, pcs(0, 0x80, true))...)
	sup = append(sup, segment(0x17, show, window)...)
	sup = append(sup, segment(0x14, show, []byte{0, 0, 1, 235, 128, 128, 255})...) // white, opaque
	sup = append(sup, segment(0x15, show, ods)...)
	sup = append(sup, segment(0x80, show, nil)...)
	sup = append(sup, segment(0x16, hide, pcs(1, 0x00, false))...)
	sup = append(sup, segment(0x17, hide, window)...)
	return append(sup, segment(0x80, hide, nil)...)
}
