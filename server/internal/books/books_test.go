package books

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// writeZip creates a zip file with the given entries (stored or deflated).
func writeZip(t *testing.T, p string, files map[string][]byte, order ...string) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if len(order) == 0 {
		for k := range files {
			order = append(order, k)
		}
	}
	for _, name := range order {
		w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Deflate})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(files[name]); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TinyJPEG is a valid 1×1 JPEG.
var TinyJPEG = []byte{0xFF, 0xD8, 0xFF, 0xDB, 0x00, 0x43, 0x00, 0x08, 0x06, 0x06, 0x07, 0x06, 0x05, 0x08, 0x07, 0x07,
	0x07, 0x09, 0x09, 0x08, 0x0A, 0x0C, 0x14, 0x0D, 0x0C, 0x0B, 0x0B, 0x0C, 0x19, 0x12, 0x13, 0x0F, 0x14, 0x1D, 0x1A,
	0x1F, 0x1E, 0x1D, 0x1A, 0x1C, 0x1C, 0x20, 0x24, 0x2E, 0x27, 0x20, 0x22, 0x2C, 0x23, 0x1C, 0x1C, 0x28, 0x37, 0x29,
	0x2C, 0x30, 0x31, 0x34, 0x34, 0x34, 0x1F, 0x27, 0x39, 0x3D, 0x38, 0x32, 0x3C, 0x2E, 0x33, 0x34, 0x32, 0xFF, 0xC0,
	0x00, 0x0B, 0x08, 0x00, 0x01, 0x00, 0x01, 0x01, 0x01, 0x11, 0x00, 0xFF, 0xC4, 0x00, 0x14, 0x00, 0x01, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x09, 0xFF, 0xC4, 0x00, 0x14, 0x10,
	0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xDA,
	0x00, 0x08, 0x01, 0x01, 0x00, 0x00, 0x3F, 0x00, 0x2A, 0x9F, 0xFF, 0xD9}

const epub3OPF = `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid">
 <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:identifier id="uid">urn:uuid:1234</dc:identifier>
  <dc:identifier>urn:isbn:978-0-306-40615-7</dc:identifier>
  <dc:title id="t1">The   Long Way</dc:title>
  <dc:creator id="c1">Ana Novak</dc:creator>
  <meta refines="#c1" property="role" scheme="marc:relators">aut</meta>
  <dc:creator id="c2">Ivo Ilustrator</dc:creator>
  <meta refines="#c2" property="role" scheme="marc:relators">ill</meta>
  <dc:language>hr</dc:language>
  <dc:publisher>Naklada</dc:publisher>
  <dc:date>2019-04-01</dc:date>
  <dc:subject>Fantasy</dc:subject>
  <dc:description>&lt;p&gt;First &lt;b&gt;line&lt;/b&gt;.&lt;/p&gt;&lt;p&gt;Second.&lt;/p&gt;</dc:description>
  <meta property="belongs-to-collection" id="s1">Wayfarers</meta>
  <meta refines="#s1" property="collection-type">series</meta>
  <meta refines="#s1" property="group-position">2</meta>
 </metadata>
 <manifest>
  <item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/>
  <item id="img" href="images/front.jpg" media-type="image/jpeg" properties="cover-image"/>
  <item id="ch1" href="text/ch1.xhtml" media-type="application/xhtml+xml"/>
 </manifest>
 <spine><itemref idref="ch1"/></spine>
</package>`

const epub2OPF = `<?xml version="1.0"?>
<package version="2.0" xmlns="http://www.idpf.org/2007/opf" xmlns:opf="http://www.idpf.org/2007/opf">
 <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title>Old Book</dc:title>
  <dc:creator opf:role="aut">First Writer</dc:creator>
  <dc:creator opf:role="edt">Some Editor</dc:creator>
  <dc:creator>Second Writer</dc:creator>
  <dc:identifier opf:scheme="ISBN">0-306-40615-2</dc:identifier>
  <meta name="cover" content="cov"/>
  <meta name="calibre:series" content="Saga"/>
  <meta name="calibre:series_index" content="3.5"/>
 </metadata>
 <manifest><item id="cov" href="cover.png" media-type="image/png"/></manifest>
 <spine page-progression-direction="rtl"/>
</package>`

func TestParseOPF(t *testing.T) {
	o, err := ParseOPF([]byte(epub3OPF))
	if err != nil {
		t.Fatal(err)
	}
	if o.Title != "The Long Way" || !reflect.DeepEqual(o.Authors, []string{"Ana Novak"}) || o.ISBN != "9780306406157" ||
		o.Language != "hr" || o.Publisher != "Naklada" || o.Year != 2019 || o.Series != "Wayfarers" ||
		o.SeriesIndex == nil || *o.SeriesIndex != 2 || o.CoverHref != "images/front.jpg" || o.Genres[0] != "Fantasy" {
		t.Fatalf("epub3: %+v", o)
	}
	if o.Description != "First line.\n\nSecond." {
		t.Fatalf("description %q", o.Description)
	}
	o, err = ParseOPF([]byte(epub2OPF))
	if err != nil {
		t.Fatal(err)
	}
	if o.Title != "Old Book" || !reflect.DeepEqual(o.Authors, []string{"First Writer", "Second Writer"}) || o.ISBN != "0306406152" ||
		o.Series != "Saga" || *o.SeriesIndex != 3.5 || o.CoverHref != "cover.png" || !o.RightToLeft {
		t.Fatalf("epub2: %+v", o)
	}
}

func TestEPUBReadAndCover(t *testing.T) {
	p := filepath.Join(t.TempDir(), "book.epub")
	writeZip(t, p, map[string][]byte{
		"mimetype":               []byte("application/epub+zip"),
		"META-INF/container.xml": []byte(`<container><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`),
		"OEBPS/content.opf":      []byte(epub3OPF),
		"OEBPS/images/front.jpg": TinyJPEG,
	}, "mimetype", "META-INF/container.xml", "OEBPS/content.opf", "OEBPS/images/front.jpg")
	in, err := Read(p)
	if err != nil || in.Format != "epub" || in.Title != "The Long Way" {
		t.Fatalf("%+v %v", in, err)
	}
	c, err := Cover(p)
	if err != nil || !bytes.Equal(c, TinyJPEG) {
		t.Fatalf("cover: %v (%d bytes)", err, len(c))
	}
}

func TestEPUBRootfileTraversalRejected(t *testing.T) {
	p := filepath.Join(t.TempDir(), "evil.epub")
	writeZip(t, p, map[string][]byte{
		"META-INF/container.xml": []byte(`<container><rootfiles><rootfile full-path="../../etc/passwd" media-type="application/oebps-package+xml"/></rootfiles></container>`),
	})
	if _, err := Read(p); !errors.Is(err, ErrUnsafeName) {
		t.Fatalf("want ErrUnsafeName, got %v", err)
	}
}

const comicInfoXML = `<?xml version="1.0"?>
<ComicInfo xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance">
  <Title>The Return</Title><Series>Space Cats</Series><Number>12</Number><Volume>2</Volume>
  <Summary>Cats in space.</Summary><Year>2021</Year><Month>3</Month><Day>9</Day>
  <Writer>Mia Writer, Leo Co-Writer</Writer><Publisher>Indie</Publisher><Genre>Sci-Fi; Humor</Genre>
  <LanguageISO>en</LanguageISO><PageCount>99</PageCount><Manga>YesAndRightToLeft</Manga><GTIN>9780306406157</GTIN>
</ComicInfo>`

func TestParseComicInfo(t *testing.T) {
	in, err := ParseComicInfo([]byte(comicInfoXML))
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "The Return" || in.Series != "Space Cats" || *in.Chapter != 12 || *in.Volume != 2 || *in.SeriesIndex != 12 ||
		!reflect.DeepEqual(in.Authors, []string{"Mia Writer", "Leo Co-Writer"}) || !reflect.DeepEqual(in.Genres, []string{"Sci-Fi", "Humor"}) ||
		!in.RightToLeft || in.Published != "2021-03-09" || in.ISBN != "9780306406157" || in.Language != "en" {
		t.Fatalf("%+v", in)
	}
}

func TestComicArchiveCBZ(t *testing.T) {
	p := filepath.Join(t.TempDir(), "Space Cats 012.cbz")
	writeZip(t, p, map[string][]byte{
		"ComicInfo.xml":          []byte(comicInfoXML),
		"page10.jpg":             []byte("ten"),
		"page2.jpg":              []byte("two"),
		"page1.jpg":              []byte("one"),
		"__MACOSX/._page1.jpg":   []byte("fork"),
		".hidden.jpg":            []byte("hidden"),
		"notes.txt":              []byte("x"),
		"../outside.jpg":         []byte("evil"),
		"/abs.jpg":               []byte("evil"),
		"sub/..\\..\\escape.png": []byte("evil"),
	})
	in, err := Read(p)
	if err != nil {
		t.Fatal(err)
	}
	// ComicInfo said 99, but the archive has three pages.
	if in.PageCount != 3 || in.Format != "cbz" || in.Series != "Space Cats" {
		t.Fatalf("%+v", in)
	}
	a, err := OpenArchive(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	if got := Pages(a.Entries()); !reflect.DeepEqual(got, []string{"page1.jpg", "page2.jpg", "page10.jpg"}) {
		t.Fatalf("pages %v", got)
	}
	data, typ, err := ReadPage(a, 2)
	if err != nil || string(data) != "ten" || typ != "image/jpeg" {
		t.Fatalf("page: %q %s %v", data, typ, err)
	}
	if _, _, err := ReadPage(a, 3); !errors.Is(err, ErrNoSuchPage) {
		t.Fatal(err)
	}
	if _, err := a.ReadFile("../outside.jpg", MaxEntryBytes); err == nil {
		t.Fatal("unsafe entry must not be readable")
	}
}

func TestZipBombIsRefused(t *testing.T) {
	p := filepath.Join(t.TempDir(), "bomb.cbz")
	// 8 MiB of zeros compresses to a few KiB: far past the allowed ratio.
	writeZip(t, p, map[string][]byte{"001.jpg": make([]byte, 8<<20)})
	a, err := OpenArchive(p)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = a.Close() }()
	if _, _, err := ReadPage(a, 0); !errors.Is(err, ErrSuspiciousRate) {
		t.Fatalf("want ErrSuspiciousRate, got %v", err)
	}
	// An entry whose declared size lies is cut off by the reader's limit.
	if _, err := readLimited(bytes.NewReader(make([]byte, 100)), 10); !errors.Is(err, ErrEntryTooLarge) {
		t.Fatal(err)
	}
	if err := checkEntry(Entry{Size: MaxEntryBytes + 1, CompressedSize: MaxEntryBytes}, MaxEntryBytes); !errors.Is(err, ErrEntryTooLarge) {
		t.Fatal(err)
	}
}

func TestTooManyEntries(t *testing.T) {
	p := filepath.Join(t.TempDir(), "many.cbz")
	files := map[string][]byte{}
	for i := 0; i <= MaxEntries; i++ {
		files[fmt.Sprintf("%05d.jpg", i)] = nil
	}
	writeZip(t, p, files)
	if _, err := OpenArchive(p); !errors.Is(err, ErrTooManyEntries) {
		t.Fatalf("want ErrTooManyEntries, got %v", err)
	}
}

func TestComicArchiveCBT(t *testing.T) {
	p := filepath.Join(t.TempDir(), "x.cbt")
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	add := func(name string, typ byte, data []byte) {
		if err := tw.WriteHeader(&tar.Header{Name: name, Typeflag: typ, Size: int64(len(data)), Mode: 0o644, Linkname: "/etc/passwd"}); err != nil {
			t.Fatal(err)
		}
		_, _ = tw.Write(data)
	}
	add("b.png", tar.TypeReg, []byte("B"))
	add("a.png", tar.TypeReg, []byte("A"))
	add("link.png", tar.TypeSymlink, nil)
	add("../up.png", tar.TypeReg, []byte("evil"))
	_ = tw.Close()
	if err := os.WriteFile(p, buf.Bytes(), 0o600); err != nil {
		t.Fatal(err)
	}
	in, err := Read(p)
	if err != nil || in.PageCount != 2 {
		t.Fatalf("%+v %v", in, err)
	}
	c, err := Cover(p)
	if err != nil || string(c) != "A" {
		t.Fatalf("cover %q %v", c, err)
	}
}

func TestComicArchiveCB7(t *testing.T) {
	// testdata/sample.cb7 was made with p7zip: pages 01.jpg, 02.jpg, ComicInfo.xml.
	in, err := Read("testdata/sample.cb7")
	if err != nil {
		t.Fatal(err)
	}
	if in.PageCount != 2 || in.Series != "Seven" || in.Format != "cb7" {
		t.Fatalf("%+v", in)
	}
	c, err := Cover("testdata/sample.cb7")
	if err != nil || !bytes.Equal(c, TinyJPEG) {
		t.Fatalf("cover: %v", err)
	}
}

func TestArchiveFormatSniffsMislabelledFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "really-a-zip.cbr")
	writeZip(t, p, map[string][]byte{"1.jpg": []byte("x")})
	if f := ArchiveFormat(p); f != "zip" {
		t.Fatal(f)
	}
	rar := filepath.Join(dir, "x.cbr")
	_ = os.WriteFile(rar, []byte("Rar!\x1a\x07\x01\x00garbage"), 0o600)
	if f := ArchiveFormat(rar); f != "rar" {
		t.Fatal(f)
	}
	if _, err := OpenArchive(rar); err == nil {
		t.Fatal("corrupt rar must fail")
	}
}

func TestSafeName(t *testing.T) {
	for name, want := range map[string]bool{"a/b.jpg": true, "a..b.jpg": true, "../a.jpg": false, "a/../../b": false,
		"/etc/x": false, "C:\\x.jpg": false, "a\\..\\..\\b": false, "x\x00.jpg": false, "": false} {
		if SafeName(name) != want {
			t.Errorf("%q: want %v", name, want)
		}
	}
}

func TestNaturalLess(t *testing.T) {
	if !NaturalLess("p2.jpg", "p10.jpg") || NaturalLess("p10.jpg", "p2.jpg") || !NaturalLess("A1", "b1") || !NaturalLess("p02", "p3") {
		t.Fatal("natural order")
	}
}

func TestParseFileName(t *testing.T) {
	f := func(v float64) *float64 { return &v }
	cases := []struct {
		in   string
		want NameInfo
	}{
		{"Saga v03 c017 (2014)", NameInfo{Series: "Saga", Volume: f(3), Chapter: f(17), Year: 2014}},
		{"One Piece - Vol. 12", NameInfo{Series: "One Piece", Volume: f(12)}},
		{"Attack_on_Titan_Chapter_045.5", NameInfo{Series: "Attack on Titan", Chapter: f(45.5)}},
		{"Batman 012 (2016) (Digital) [Group]", NameInfo{Series: "Batman", Chapter: f(12), Year: 2016}},
		{"Spider-Man #5", NameInfo{Series: "Spider-Man", Chapter: f(5)}},
		{"1984", NameInfo{Series: "1984"}},
		{"The Hobbit", NameInfo{Series: "The Hobbit"}},
	}
	for _, c := range cases {
		got := ParseFileName(c.in)
		if got.Series != c.want.Series || got.Year != c.want.Year || !reflect.DeepEqual(got.Volume, c.want.Volume) || !reflect.DeepEqual(got.Chapter, c.want.Chapter) {
			t.Errorf("%q: got %+v (vol %v ch %v)", c.in, got, deref(got.Volume), deref(got.Chapter))
		}
	}
}

func deref(p *float64) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestNormalizeISBN(t *testing.T) {
	for in, want := range map[string]string{"978-0-306-40615-7": "9780306406157", "urn:isbn:0306406152": "0306406152",
		"080442957X": "080442957X", "9780306406158": "", "abc": "", "ISBN 0-8044-2957-X": "080442957X"} {
		if got := NormalizeISBN(in); got != want {
			t.Errorf("%q: %q want %q", in, got, want)
		}
	}
}

func TestParsePDFPlain(t *testing.T) {
	pdf := "%PDF-1.4\n1 0 obj\n<< /Type /Catalog /Pages 2 0 R >>\nendobj\n" +
		"2 0 obj\n<< /Kids [3 0 R 4 0 R] /Type /Pages /Count 5 >>\nendobj\n" +
		"3 0 obj\n<< /Type /Pages /Parent 2 0 R /Count 2 /Kids [] >>\nendobj\n" +
		"9 0 obj\n<< /Producer (Test) /Title (Caf\\351 \\(draft\\)) /Author <FEFF0041006E0061> >>\nendobj\n" +
		"trailer << /Root 1 0 R /Info 9 0 R >>\n%%EOF\n"
	in, err := ParsePDF(strings.NewReader(pdf), int64(len(pdf)))
	if err != nil {
		t.Fatal(err)
	}
	if in.PageCount != 5 || in.Title != "Café (draft)" || !reflect.DeepEqual(in.Authors, []string{"Ana"}) {
		t.Fatalf("%+v", in)
	}
}

func TestParsePDFObjectStream(t *testing.T) {
	var z bytes.Buffer
	zw := zlib.NewWriter(&z)
	_, _ = zw.Write([]byte("2 0 3 50 << /Type /Pages /Kids [3 0 R] /Count 42 >> << /Type /Catalog /Pages 2 0 R >>"))
	_ = zw.Close()
	pdf := "%PDF-1.7\n5 0 obj\n<< /Type /ObjStm /N 2 /First 8 /Filter /FlateDecode /Length " + fmt.Sprint(z.Len()) + " >>\nstream\r\n" +
		z.String() + "\nendstream\nendobj\n%%EOF\n"
	in, err := ParsePDF(strings.NewReader(pdf), int64(len(pdf)))
	if err != nil || in.PageCount != 42 {
		t.Fatalf("%+v %v", in, err)
	}
	if _, err := ParsePDF(strings.NewReader("not a pdf"), 9); err == nil {
		t.Fatal("want error")
	}
}

// buildMOBI writes a minimal PalmDB/MOBI file with EXTH metadata and a cover record.
func buildMOBI(title string, exth map[int][]byte, cover []byte) []byte {
	var ex bytes.Buffer
	for _, typ := range []int{100, 104, 201, 524} {
		d, ok := exth[typ]
		if !ok {
			continue
		}
		_ = binary.Write(&ex, binary.BigEndian, uint32(typ))
		_ = binary.Write(&ex, binary.BigEndian, uint32(8+len(d)))
		ex.Write(d)
	}
	count := len(exth)
	exthBlock := append([]byte("EXTH"), make([]byte, 8)...)
	binary.BigEndian.PutUint32(exthBlock[4:], uint32(12+ex.Len()))
	binary.BigEndian.PutUint32(exthBlock[8:], uint32(count))
	exthBlock = append(exthBlock, ex.Bytes()...)
	const headerLen = 232
	r0 := make([]byte, 16+headerLen)
	copy(r0[16:], "MOBI")
	binary.BigEndian.PutUint32(r0[20:], headerLen)
	binary.BigEndian.PutUint32(r0[28:], 65001)
	binary.BigEndian.PutUint32(r0[108:], 1) // first image record
	binary.BigEndian.PutUint32(r0[128:], 0x40)
	r0 = append(r0, exthBlock...)
	binary.BigEndian.PutUint32(r0[84:], uint32(len(r0)))
	binary.BigEndian.PutUint32(r0[88:], uint32(len(title)))
	r0 = append(r0, []byte(title)...)
	hdr := make([]byte, 78+16)
	copy(hdr[60:], "BOOKMOBI")
	binary.BigEndian.PutUint16(hdr[76:], 2)
	binary.BigEndian.PutUint32(hdr[78:], uint32(len(hdr)))
	binary.BigEndian.PutUint32(hdr[86:], uint32(len(hdr)+len(r0)))
	out := append(hdr, r0...)
	return append(out, cover...)
}

func TestParseMOBI(t *testing.T) {
	data := buildMOBI("Ignored Full Name", map[int][]byte{100: []byte("Ana Novak; Ivo Drugi"), 104: []byte("978-0-306-40615-7"),
		201: {0, 0, 0, 0}, 524: []byte("hr")}, TinyJPEG)
	in, cover, err := ParseMOBI(bytes.NewReader(data), int64(len(data)), true)
	if err != nil {
		t.Fatal(err)
	}
	if in.Title != "Ignored Full Name" || !reflect.DeepEqual(in.Authors, []string{"Ana Novak", "Ivo Drugi"}) || in.ISBN != "9780306406157" ||
		in.Language != "hr" || !bytes.Equal(cover, TinyJPEG) {
		t.Fatalf("%+v cover=%d", in, len(cover))
	}
	if _, _, err := ParseMOBI(bytes.NewReader([]byte("short")), 5, false); err == nil {
		t.Fatal("want error")
	}
}
