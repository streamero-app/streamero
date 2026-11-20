package books

import (
	"bytes"
	"testing"
	"time"
)

// Fuzz targets for the parsers that read files from the media folders, which
// anyone able to drop a file there controls. The seeds run with every go test;
// `go test -fuzz=FuzzParsePDF ./internal/books` explores further. Each target
// must return (with or without an error) quickly and without panicking.

func finishesQuickly(t *testing.T, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		fn()
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("parser did not finish within 5 s")
	}
}

func FuzzParsePDF(f *testing.F) {
	f.Add([]byte("%PDF-1.7\n1 0 obj << /Type /Catalog /Pages 2 0 R >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF"))
	f.Add([]byte("%PDF-1.4\nxref\n0 99999999\ntrailer << /Size 99999999 /Root 1 0 R /Info 1 0 R >>\nstartxref\n9\n%%EOF"))
	f.Add([]byte("%PDF-1.5\n1 0 obj << /Type /ObjStm /N 2147483647 /First -5 /Length 3 >> stream\nabc\nendstream endobj\nstartxref\n0\n%%EOF"))
	f.Add([]byte("%PDF-1.4\n1 0 obj << /Pages 1 0 R /Kids [1 0 R] /Count 1 >> endobj\ntrailer << /Root 1 0 R >>\n%%EOF"))
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, data []byte) {
		finishesQuickly(t, func() { _, _ = ParsePDF(bytes.NewReader(data), int64(len(data))) })
	})
}

func FuzzParseMOBI(f *testing.F) {
	hdr := make([]byte, 78+16)
	copy(hdr[60:], "BOOKMOBI")
	hdr[76], hdr[77] = 0xff, 0xff // 65535 records in a tiny file
	f.Add(hdr)
	f.Add(make([]byte, 100))
	f.Add([]byte("BOOKMOBI"))
	f.Fuzz(func(t *testing.T, data []byte) {
		finishesQuickly(t, func() { _, _, _ = ParseMOBI(bytes.NewReader(data), int64(len(data)), true) })
	})
}

func FuzzParseOPF(f *testing.F) {
	f.Add([]byte(`<?xml version="1.0"?><package><metadata><dc:title>T</dc:title></metadata></package>`))
	f.Add([]byte(`<!DOCTYPE x [<!ENTITY a "aaaaaaaaaa"><!ENTITY b "&a;&a;&a;&a;&a;&a;&a;&a;">]><package><metadata><dc:title>&b;</dc:title></metadata></package>`))
	f.Add([]byte(`<package>` + string(bytes.Repeat([]byte("<a>"), 10000))))
	f.Fuzz(func(t *testing.T, data []byte) {
		finishesQuickly(t, func() {
			_, _ = ParseOPF(data)
			_, _ = ParseComicInfo(data)
		})
	})
}

func FuzzFileName(f *testing.F) {
	f.Add("Series v01 c001 (2020)")
	f.Add("../../etc/passwd")
	f.Add(string(bytes.Repeat([]byte("9"), 400)))
	f.Fuzz(func(t *testing.T, s string) {
		_ = ParseFileName(s)
		_ = NormalizeISBN(s)
		_ = StripHTML(s)
		_ = ResolveHref("OEBPS/content.opf", s)
		if SafeName(s) && (bytes.HasPrefix([]byte(s), []byte("/")) || bytes.Contains([]byte("/"+s+"/"), []byte("/../"))) {
			t.Fatalf("SafeName accepted %q", s)
		}
	})
}
