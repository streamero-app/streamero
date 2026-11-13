package app

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
)

// --- fixtures ----------------------------------------------------------------------

func writeFile(t *testing.T, p string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func testJPEG(t *testing.T, w, h int, c color.Color) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func zipBytes(t *testing.T, files [][2]any) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, f := range files {
		w, err := zw.Create(f[0].(string))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = w.Write(f[1].([]byte))
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func testEPUB(t *testing.T, title, author, series string, index int, cover []byte) []byte {
	opf := fmt.Sprintf(`<?xml version="1.0"?>
<package xmlns="http://www.idpf.org/2007/opf" version="2.0" xmlns:opf="http://www.idpf.org/2007/opf">
 <metadata xmlns:dc="http://purl.org/dc/elements/1.1/">
  <dc:title>%s</dc:title><dc:creator opf:role="aut">%s</dc:creator><dc:language>en</dc:language>
  <dc:identifier opf:scheme="ISBN">9780306406157</dc:identifier>
  <dc:description>A book about testing.</dc:description>
  <meta name="calibre:series" content="%s"/><meta name="calibre:series_index" content="%d"/>
  <meta name="cover" content="cov"/>
 </metadata>
 <manifest><item id="cov" href="cover.jpg" media-type="image/jpeg"/>
  <item id="c1" href="c1.xhtml" media-type="application/xhtml+xml"/></manifest>
 <spine><itemref idref="c1"/></spine>
</package>`, title, author, series, index)
	return zipBytes(t, [][2]any{
		{"mimetype", []byte("application/epub+zip")},
		{"META-INF/container.xml", []byte(`<container><rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles></container>`)},
		{"OEBPS/content.opf", []byte(opf)},
		{"OEBPS/cover.jpg", cover},
		{"OEBPS/c1.xhtml", []byte(`<html xmlns="http://www.w3.org/1999/xhtml"><body><p>Hello.</p></body></html>`)},
	})
}

// testPDF writes a small valid PDF with n pages (correct cross-reference table).
func testPDF(n int) []byte {
	var objs []string
	kids := []string{}
	for i := 0; i < n; i++ {
		kids = append(kids, fmt.Sprintf("%d 0 R", 3+i))
	}
	objs = append(objs, "<< /Type /Catalog /Pages 2 0 R >>",
		fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), n))
	for i := 0; i < n; i++ {
		objs = append(objs, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 200 300] >>")
	}
	objs = append(objs, "<< /Title (Plain Document) /Author (Pat Doe) /Producer (test) >>")
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := []int{}
	for i, o := range objs {
		offsets = append(offsets, b.Len())
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, len(objs), xref)
	return b.Bytes()
}

// exifJPEG adds an EXIF block (date taken, camera, orientation, GPS) to a JPEG.
func exifJPEG(jpg []byte, taken string, orientation uint16) []byte {
	le := binary.LittleEndian
	entry := func(tag, typ uint16, count, value uint32) []byte {
		e := make([]byte, 12)
		le.PutUint16(e, tag)
		le.PutUint16(e[2:], typ)
		le.PutUint32(e[4:], count)
		le.PutUint32(e[8:], value)
		return e
	}
	// Layout: header(8) | IFD0 (5 entries) @8 | Exif IFD (1) | GPS IFD (4) | data
	ifd0 := 8
	exifIFD := ifd0 + 2 + 5*12 + 4
	gpsIFD := exifIFD + 2 + 1*12 + 4
	data := gpsIFD + 2 + 4*12 + 4
	model := "TestCam 1\x00"
	dateOff := data + len(model)
	latOff := dateOff + 20
	lonOff := latOff + 24
	var b bytes.Buffer
	b.Write([]byte{'I', 'I', 42, 0, 8, 0, 0, 0})
	b.Write([]byte{5, 0})
	b.Write(entry(0x0110, 2, uint32(len(model)), uint32(data)))
	b.Write(entry(0x0112, 3, 1, uint32(orientation)))
	b.Write(entry(0x8769, 4, 1, uint32(exifIFD)))
	b.Write(entry(0x8825, 4, 1, uint32(gpsIFD)))
	b.Write(entry(0x0131, 2, 1, 0))
	b.Write([]byte{0, 0, 0, 0})
	b.Write([]byte{1, 0})
	b.Write(entry(0x9003, 2, 20, uint32(dateOff)))
	b.Write([]byte{0, 0, 0, 0})
	b.Write([]byte{4, 0})
	b.Write(entry(1, 2, 2, 'N'))
	b.Write(entry(2, 5, 3, uint32(latOff)))
	b.Write(entry(3, 2, 2, 'E'))
	b.Write(entry(4, 5, 3, uint32(lonOff)))
	b.Write([]byte{0, 0, 0, 0})
	b.WriteString(model)
	b.WriteString(taken + "\x00")
	for _, v := range []uint32{45, 1, 30, 1, 0, 1, 16, 1, 0, 1, 0, 1} {
		_ = binary.Write(&b, le, v)
	}
	seg := append([]byte("Exif\x00\x00"), b.Bytes()...)
	app1 := []byte{0xFF, 0xE1, 0, 0}
	binary.BigEndian.PutUint16(app1[2:], uint16(len(seg)+2))
	out := append([]byte{}, jpg[:2]...)
	out = append(out, app1...)
	out = append(out, seg...)
	return append(out, jpg[2:]...)
}

func mkdirFor(t *testing.T, p string) string {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func ffmpegRun(t *testing.T, ffmpeg string, args ...string) {
	t.Helper()
	out, err := exec.Command(ffmpeg, append([]string{"-y", "-loglevel", "error"}, args...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("ffmpeg %v: %v %s", args, err, out)
	}
}

// --- test ------------------------------------------------------------------------------

// TestBooksAudiobooksPhotosIntegration scans generated EPUB, CBZ, PDF, M4B, MP3 and
// JPEG files into real libraries and walks the API: metadata, authors and series,
// readers' manifests and pages, reading and listening progress, photo albums and
// EXIF, generated covers, and that the compat API does not offer these libraries.
func TestBooksAudiobooksPhotosIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	ctx := context.Background()
	ffmpeg, _ := exec.LookPath("ffmpeg")
	ffprobe, _ := exec.LookPath("ffprobe")
	imageDir := t.TempDir()
	testState.Library().ImageDir = imageDir
	// Settings are shared with other tests: start (and end) with photo locations off.
	privacy := map[string]any{"language": "en-US", "country": "US", "photoLocation": false}
	adminC.do("PUT", "/settings/metadata", privacy, nil)
	t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `UPDATE settings SET value = value - 'photoLocation' WHERE key = 'metadata'`) })

	// Books: an EPUB series by one author, a comic with ComicInfo.xml, a PDF.
	booksRoot := mediaRoot + "/books"
	red := testJPEG(t, 60, 90, color.RGBA{200, 20, 20, 255})
	writeFile(t, booksRoot+"/Ana Novak/Wayfarers 1.epub", testEPUB(t, "The Long Way", "Ana Novak", "Wayfarers", 1, red))
	writeFile(t, booksRoot+"/Ana Novak/Wayfarers 2.epub", testEPUB(t, "A Closed Orbit", "Ana Novak", "Wayfarers", 2, red))
	pages := [][2]any{{"ComicInfo.xml", []byte(`<ComicInfo><Series>Space Cats</Series><Number>3</Number><Writer>Mia Writer</Writer><Manga>YesAndRightToLeft</Manga></ComicInfo>`)}}
	for i := 1; i <= 3; i++ {
		pages = append(pages, [2]any{fmt.Sprintf("p%02d.jpg", i), testJPEG(t, 40, 60, color.RGBA{uint8(60 * i), 90, 30, 255})})
	}
	writeFile(t, booksRoot+"/Space Cats/Space Cats 003.cbz", zipBytes(t, pages))
	writeFile(t, booksRoot+"/plain.pdf", testPDF(4))

	// Audiobooks: an M4B with chapters, and a folder of MP3 parts.
	abRoot := mediaRoot + "/audiobooks"
	if err := os.MkdirAll(abRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	if ffmpeg != "" {
		meta := mediaRoot + "/chapters.txt"
		writeFile(t, meta, []byte(";FFMETADATA1\n[CHAPTER]\nTIMEBASE=1/1000\nSTART=0\nEND=3000\ntitle=Opening\n"+
			"[CHAPTER]\nTIMEBASE=1/1000\nSTART=3000\nEND=6000\ntitle=Closing\n"))
		ffmpegRun(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=440:duration=6", "-i", meta, "-map_metadata", "1", "-map", "0",
			"-metadata", "album=The Sound Book", "-metadata", "artist=Lea Author", "-metadata", "composer=Nina Narrator",
			"-c:a", "aac", "-b:a", "48k", "-f", "mp4", mkdirFor(t, abRoot+"/Lea Author/The Sound Book.m4b"))
		for i, name := range []string{"02 - Second.mp3", "01 - First.mp3"} {
			p := abRoot + "/Max Author/Folder Book/" + name
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			ffmpegRun(t, ffmpeg, "-f", "lavfi", "-i", "sine=frequency=330:duration=3", "-metadata", "album=Folder Book",
				"-metadata", fmt.Sprintf("track=%d", 2-i), "-metadata", "title="+strings.TrimSuffix(name[5:], ".mp3"), "-c:a", "libmp3lame", p)
		}
	}

	// Photos: an album with a sub-album, EXIF dates and a picture at the root.
	phRoot := mediaRoot + "/photos"
	writeFile(t, phRoot+"/Summer/beach.jpg", exifJPEG(testJPEG(t, 80, 40, color.RGBA{20, 120, 200, 255}), "2023:07:14 18:30:05", 6))
	writeFile(t, phRoot+"/Summer/hills.jpg", exifJPEG(testJPEG(t, 80, 40, color.RGBA{20, 160, 20, 255}), "2023:07:10 09:00:00", 1))
	writeFile(t, phRoot+"/Summer/Day trip/boat.jpg", exifJPEG(testJPEG(t, 50, 50, color.RGBA{220, 220, 220, 255}), "2023:07:20 12:00:00", 1))
	writeFile(t, phRoot+"/loose.jpg", testJPEG(t, 30, 30, color.RGBA{0, 0, 0, 255}))

	newLib := func(name, kind, root string) uuid.UUID {
		var lib struct {
			ID string `json:"id"`
		}
		if s := adminC.do("POST", "/libraries", map[string]any{"name": name, "kind": kind, "paths": []string{root}}, &lib); s != 201 {
			t.Fatalf("create %s: HTTP %d", kind, s)
		}
		id := uuid.MustParse(lib.ID)
		t.Cleanup(func() { _, _ = testPool.Exec(context.Background(), `DELETE FROM libraries WHERE id = $1`, id) })
		return id
	}
	booksID := newLib("Knjige", "books", booksRoot)
	abID := newLib("Audioknjige", "audiobooks", abRoot)
	phID := newLib("Fotografije", "photos", phRoot)

	probe := library.FFprobe(ffprobe)
	if ffprobe == "" {
		probe = func(context.Context, string) (library.ProbeResult, error) { return library.ProbeResult{}, nil }
	}
	scanner := &library.Scanner{Pool: testPool, Roots: library.Roots{mediaRoot}, Log: slog.New(slog.NewTextHandler(io.Discard, nil)), Probe: probe}
	for _, id := range []uuid.UUID{booksID, abID, phID} {
		if _, err := scanner.Scan(ctx, id); err != nil {
			t.Fatal(err)
		}
	}
	refresher := &metadata.Refresher{Pool: testPool, Settings: testState.Settings(), ImageDir: imageDir, FFmpeg: ffmpeg,
		Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(library.Roots{mediaRoot}, p) }}
	for _, id := range []uuid.UUID{booksID, abID, phID} {
		if _, err := refresher.MediaArtwork(ctx, id); err != nil {
			t.Fatal(err)
		}
	}

	type summary struct {
		ID          string   `json:"id"`
		Kind        string   `json:"kind"`
		Title       string   `json:"title"`
		Authors     []string `json:"authors"`
		SeriesName  string   `json:"seriesName"`
		SeriesIndex float64  `json:"seriesIndex"`
		BookFormat  string   `json:"bookFormat"`
		Progression *float64 `json:"progression"`
		TakenAt     string   `json:"takenAt"`
		Width       int      `json:"width"`
		Height      int      `json:"height"`
		ChildCount  int      `json:"childCount"`
		HasPoster   bool     `json:"hasPoster"`
		PositionMs  int64    `json:"positionMs"`
		DurationMs  int64    `json:"durationMs"`
	}
	type page struct {
		Items []summary `json:"items"`
		Total int       `json:"total"`
	}
	byTitle := func(items []summary) map[string]summary {
		m := map[string]summary{}
		for _, it := range items {
			m[it.Title] = it
		}
		return m
	}

	// --- books ---
	var books page
	if s := adminC.do("GET", "/libraries/"+booksID.String()+"/items?sort=series", nil, &books); s != 200 || books.Total != 4 {
		t.Fatalf("books: HTTP %d %+v", s, books)
	}
	bt := byTitle(books.Items)
	long, comic, pdf := bt["The Long Way"], bt["Space Cats 003"], bt["Plain Document"]
	if long.Kind != "book" || long.BookFormat != "epub" || long.SeriesName != "Wayfarers" || long.SeriesIndex != 1 ||
		len(long.Authors) != 1 || long.Authors[0] != "Ana Novak" {
		t.Fatalf("epub: %+v", long)
	}
	if comic.BookFormat != "cbz" || comic.SeriesName != "Space Cats" || comic.SeriesIndex != 3 || comic.Authors[0] != "Mia Writer" {
		t.Fatalf("comic: %+v (%v)", comic, bt)
	}
	if pdf.BookFormat != "pdf" || pdf.Authors[0] != "Pat Doe" {
		t.Fatalf("pdf: %+v", pdf)
	}
	if ffmpeg != "" && (!long.HasPoster || !comic.HasPoster) {
		t.Fatalf("covers from EPUB and first comic page: epub %v comic %v", long.HasPoster, comic.HasPoster)
	}
	if _, err := exec.LookPath("pdftoppm"); err == nil && !pdf.HasPoster {
		t.Fatal("PDF cover from the first page")
	}
	var authors, series []struct {
		Name  string `json:"name"`
		Count int    `json:"count"`
	}
	adminC.do("GET", "/libraries/"+booksID.String()+"/authors", nil, &authors)
	adminC.do("GET", "/libraries/"+booksID.String()+"/series", nil, &series)
	if len(authors) != 3 || authors[0].Name != "Ana Novak" || authors[0].Count != 2 || len(series) != 2 || series[1].Name != "Wayfarers" || series[1].Count != 2 {
		t.Fatalf("authors %+v series %+v", authors, series)
	}
	var filtered page
	adminC.do("GET", "/libraries/"+booksID.String()+"/items?author=Ana%20Novak&sort=series", nil, &filtered)
	if filtered.Total != 2 || filtered.Items[0].Title != "The Long Way" || filtered.Items[1].Title != "A Closed Orbit" {
		t.Fatalf("by author: %+v", filtered)
	}
	var detail struct {
		Overview string `json:"overview"`
		Book     struct {
			Format      string `json:"format"`
			Isbn        string `json:"isbn"`
			PageCount   int    `json:"pageCount"`
			RightToLeft bool   `json:"rightToLeft"`
		} `json:"book"`
	}
	adminC.do("GET", "/items/"+long.ID, nil, &detail)
	if detail.Book.Isbn != "9780306406157" || detail.Overview != "A book about testing." {
		t.Fatalf("epub detail: %+v", detail)
	}
	adminC.do("GET", "/items/"+pdf.ID, nil, &detail)
	if detail.Book.PageCount != 4 {
		t.Fatalf("pdf pages: %+v", detail)
	}

	// Comic reader: signed page URLs that serve images out of the archive.
	var manifest struct {
		Format      string   `json:"format"`
		FileURL     string   `json:"fileUrl"`
		PageCount   int      `json:"pageCount"`
		Pages       []string `json:"pages"`
		RightToLeft bool     `json:"rightToLeft"`
		Progress    struct {
			Page        *int    `json:"page"`
			Progression float64 `json:"progression"`
			Finished    bool    `json:"finished"`
		} `json:"progress"`
	}
	if s := adminC.do("GET", "/items/"+comic.ID+"/read", nil, &manifest); s != 200 || manifest.PageCount != 3 || len(manifest.Pages) != 3 || !manifest.RightToLeft {
		t.Fatalf("comic manifest: HTTP %d %+v", s, manifest)
	}
	get := func(u string) (int, string, []byte) {
		resp, err := http.Get(srv.URL + u)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = resp.Body.Close() }()
		b, _ := io.ReadAll(resp.Body)
		return resp.StatusCode, resp.Header.Get("Content-Type"), b
	}
	if s, ct, b := get(manifest.Pages[2]); s != 200 || ct != "image/jpeg" || !bytes.Equal(b, pages[3][1].([]byte)) {
		t.Fatalf("page 3: %d %s %d bytes", s, ct, len(b))
	}
	if s, _, _ := get(strings.Replace(manifest.Pages[0], "/0?", "/7?", 1)); s != 404 {
		t.Fatalf("missing page: %d", s)
	}
	if s, _, _ := get(strings.SplitN(manifest.Pages[0], "?", 2)[0] + "?exp=1&sig=x"); s != 403 {
		t.Fatalf("unsigned page: %d", s)
	}
	adminC.do("GET", "/items/"+pdf.ID+"/read", nil, &manifest)
	if s, ct, b := get(manifest.FileURL); s != 200 || ct != "application/pdf" || !bytes.HasPrefix(b, []byte("%PDF")) {
		t.Fatalf("pdf file: %d %s", s, ct)
	}

	// Reading progress is per profile and drives "continue reading".
	if s := adminC.do("PUT", "/items/"+comic.ID+"/reading-progress", map[string]any{"page": 1, "progression": 0.5}, nil); s != 204 {
		t.Fatalf("save progress: HTTP %d", s)
	}
	if s := adminC.do("PUT", "/items/"+long.ID+"/reading-progress", map[string]any{"locator": "epubcfi(/6/4!/4/2)", "progression": 0.25}, nil); s != 204 {
		t.Fatalf("save epub progress: HTTP %d", s)
	}
	var cont []summary
	adminC.do("GET", "/home/continue-reading", nil, &cont)
	if len(cont) != 2 || cont[0].ID != long.ID || cont[0].Progression == nil || *cont[0].Progression != 0.25 {
		t.Fatalf("continue reading: %+v", cont)
	}
	adminC.do("GET", "/items/"+comic.ID+"/read", nil, &manifest)
	if manifest.Progress.Page == nil || *manifest.Progress.Page != 1 || manifest.Progress.Progression != 0.5 {
		t.Fatalf("saved page: %+v", manifest.Progress)
	}
	adminC.do("PUT", "/items/"+comic.ID+"/reading-progress", map[string]any{"page": 2, "progression": 0.9}, nil)
	adminC.do("GET", "/items/"+comic.ID+"/reading-progress", nil, &manifest.Progress)
	if !manifest.Progress.Finished {
		t.Fatalf("last page marks the comic read: %+v", manifest.Progress)
	}
	if s := adminC.do("PUT", "/items/"+comic.ID+"/reading-progress", map[string]any{"progression": 2}, nil); s != 400 {
		t.Fatalf("progression over 1: HTTP %d", s)
	}

	// --- audiobooks ---
	if ffmpeg != "" && ffprobe != "" {
		var abs page
		adminC.do("GET", "/libraries/"+abID.String()+"/items", nil, &abs)
		at := byTitle(abs.Items)
		m4b, folder := at["The Sound Book"], at["Folder Book"]
		if abs.Total != 2 || m4b.Kind != "audiobook" || m4b.Authors[0] != "Lea Author" || folder.Authors[0] != "Max Author" {
			t.Fatalf("audiobooks: %+v", abs)
		}
		var ad struct {
			Book struct {
				Narrators []string `json:"narrators"`
			} `json:"book"`
		}
		adminC.do("GET", "/items/"+m4b.ID, nil, &ad)
		if len(ad.Book.Narrators) != 1 || ad.Book.Narrators[0] != "Nina Narrator" {
			t.Fatalf("narrator: %+v", ad)
		}
		var listen struct {
			DurationMs int64 `json:"durationMs"`
			PositionMs int64 `json:"positionMs"`
			Parts      []struct {
				FileID   string `json:"fileId"`
				Title    string `json:"title"`
				OffsetMs int64  `json:"offsetMs"`
			} `json:"parts"`
			Chapters []struct {
				Title   string `json:"title"`
				StartMs int64  `json:"startMs"`
			} `json:"chapters"`
		}
		adminC.do("GET", "/items/"+m4b.ID+"/listen", nil, &listen)
		if len(listen.Parts) != 1 || len(listen.Chapters) != 2 || listen.Chapters[1].Title != "Closing" || listen.Chapters[1].StartMs != 3000 {
			t.Fatalf("m4b: %+v", listen)
		}
		adminC.do("GET", "/items/"+folder.ID+"/listen", nil, &listen)
		if len(listen.Parts) != 2 || listen.Parts[0].Title != "First" || listen.Parts[1].OffsetMs < 2900 || len(listen.Chapters) != 2 {
			t.Fatalf("folder book: %+v", listen)
		}
		// Position in the whole book; kept even early on (no 5% resume threshold).
		adminC.do("PUT", "/items/"+folder.ID+"/progress", map[string]any{"positionMs": 3500, "durationMs": listen.DurationMs}, nil)
		adminC.do("GET", "/items/"+folder.ID+"/listen", nil, &listen)
		if listen.PositionMs != 3500 {
			t.Fatalf("listening position: %d", listen.PositionMs)
		}
		var pi struct {
			Method string `json:"method"`
			URL    string `json:"url"`
		}
		if s := adminC.do("POST", "/items/"+folder.ID+"/playback", map[string]any{"fileId": listen.Parts[1].FileID,
			"device": map[string]any{"containers": []string{"mp3"}, "videoCodecs": []string{}, "audioCodecs": []string{"mp3"}}}, &pi); s != 200 || pi.Method != "direct" {
			t.Fatalf("part playback: HTTP %d %+v", s, pi)
		}
		if s, _, b := get(pi.URL); s != 200 || len(b) < 1000 {
			t.Fatalf("part stream: %d %d", s, len(b))
		}
	}

	// --- photos ---
	var top page
	adminC.do("GET", "/libraries/"+phID.String()+"/items", nil, &top)
	pt := byTitle(top.Items)
	summer := pt["Summer"]
	if top.Total != 2 || summer.Kind != "photoalbum" || summer.ChildCount != 3 || pt["loose"].Kind != "photo" {
		t.Fatalf("photos top: %+v", top)
	}
	var album page
	adminC.do("GET", "/libraries/"+phID.String()+"/items?parentId="+summer.ID+"&sort=taken", nil, &album)
	if album.Total != 3 || album.Items[0].Kind != "photoalbum" || album.Items[1].Title != "beach" || album.Items[2].Title != "hills" {
		t.Fatalf("album: %+v", album)
	}
	beach := album.Items[1]
	// Orientation 6: an 80×40 picture shown 40×80.
	if beach.TakenAt != "2023-07-14T18:30:05Z" || beach.Width != 40 || beach.Height != 80 {
		t.Fatalf("beach: %+v", beach)
	}
	if ffmpeg != "" && (!beach.HasPoster || !summer.HasPoster) {
		t.Fatalf("photo thumbnails: photo %v album %v", beach.HasPoster, summer.HasPoster)
	}
	var pd struct {
		Photo struct {
			Camera   string   `json:"camera"`
			Latitude *float64 `json:"latitude"`
		} `json:"photo"`
	}
	adminC.do("GET", "/items/"+beach.ID, nil, &pd)
	if pd.Photo.Camera != "TestCam 1" || pd.Photo.Latitude != nil {
		t.Fatalf("GPS must stay private by default: %+v", pd)
	}
	if s := adminC.do("PUT", "/settings/metadata", map[string]any{"language": "en-US", "country": "US", "photoLocation": true}, nil); s != 200 {
		t.Fatalf("settings: HTTP %d", s)
	}
	adminC.do("GET", "/items/"+beach.ID, nil, &pd)
	if pd.Photo.Latitude == nil || *pd.Photo.Latitude != 45.5 {
		t.Fatalf("GPS when allowed: %+v", pd)
	}
	var view struct {
		URL      string `json:"url"`
		MimeType string `json:"mimeType"`
	}
	adminC.do("GET", "/items/"+beach.ID+"/photo", nil, &view)
	if s, ct, _ := get(view.URL); s != 200 || ct != "image/jpeg" || view.MimeType != "image/jpeg" {
		t.Fatalf("photo view: %d %s", s, ct)
	}

	// A rescan without changes reads nothing again.
	for _, id := range []uuid.UUID{booksID, abID, phID} {
		res, err := scanner.Scan(ctx, id)
		if err != nil || res.Probed != 0 || res.Removed != 0 {
			t.Fatalf("rescan: %+v %v", res, err)
		}
	}

	// Jellyfin-style apps are not offered these libraries (D-039).
	c := &compatCaller{t: t, base: srv.URL + "/compat"}
	var login struct{ AccessToken string }
	c.call("POST", "/Users/AuthenticateByName", map[string]any{"Username": "Admin", "Pw": "admin-pass-1"}, &login)
	c.token = login.AccessToken
	var views compatList
	c.call("GET", "/UserViews", nil, &views)
	for _, v := range views.Items {
		if v.ID == strings.ReplaceAll(booksID.String(), "-", "") || v.ID == strings.ReplaceAll(phID.String(), "-", "") {
			t.Fatalf("compat view of a book/photo library: %+v", v)
		}
	}
	var items compatList
	c.call("GET", "/Items?Recursive=true&ParentId="+strings.ReplaceAll(booksID.String(), "-", ""), nil, &items)
	if len(items.Items) != 0 {
		t.Fatalf("compat lists books: %+v", items)
	}
}
