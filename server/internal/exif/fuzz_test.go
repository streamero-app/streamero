package exif

import (
	"bytes"
	"testing"
	"time"
)

// FuzzRead feeds malformed pictures to the EXIF reader: it must return (with
// or without an error) quickly and never panic, whatever the offsets, counts
// and IFD loops in the file say. The seeds run with every go test.
func FuzzRead(f *testing.F) {
	// JPEG with an APP1 Exif segment whose IFD points at itself (a loop).
	loop := []byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x1E, 'E', 'x', 'i', 'f', 0, 0,
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // TIFF header, IFD at 8
		0x01, 0x00, // 1 entry
		0x69, 0x87, 0x04, 0x00, 0x01, 0x00, 0x00, 0x00, 0x08, 0x00, 0x00, 0x00, // ExifIFD -> 8 again
		0x08, 0x00, 0x00, 0x00, // next IFD -> 8
		0xFF, 0xD9}
	f.Add(loop)
	// Huge entry count and value offsets past the end.
	f.Add([]byte{0xFF, 0xD8, 0xFF, 0xE1, 0x00, 0x14, 'E', 'x', 'i', 'f', 0, 0,
		'M', 'M', 0x00, 0x2A, 0x00, 0x00, 0x00, 0x08, 0xFF, 0xFF, 0xFF, 0xD9})
	f.Add([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x00eXIf"))
	f.Add([]byte("RIFF\xff\xff\xff\xffWEBPEXIF\xff\xff\xff\x7f"))
	f.Add([]byte("\x00\x00\x00\x18ftypheic\x00\x00\x00\x00\xff\xff\xff\xffmeta"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, _ = Read(bytes.NewReader(data), int64(len(data)))
		}()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("EXIF reader did not finish within 5 s")
		}
	})
}
