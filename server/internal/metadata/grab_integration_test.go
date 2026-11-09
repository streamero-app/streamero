package metadata

import (
	"context"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
)

func TestGrabFramesIntegration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	pool := testPool(t)
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "media", "Frame Movie (2020)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	video := filepath.Join(dir, "Frame Movie (2020).mp4")
	if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=640x360:duration=4:rate=10",
		"-pix_fmt", "yuv420p", video).CombinedOutput(); err != nil {
		t.Fatalf("make video: %v %s", err, out)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	roots := library.Roots{root}
	svc := &library.Service{Pool: pool, Roots: roots, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	scanner := &library.Scanner{Pool: pool, Roots: roots, Log: log,
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mp4", DurationMS: 4000}, nil
		}}
	lib, err := svc.Create(ctx, "Frames", "movies", []string{filepath.Join(root, "media")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	images := filepath.Join(root, "images")
	r := &Refresher{Pool: pool, ImageDir: images, Log: log, FFmpeg: ffmpeg,
		ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }}
	n, err := r.GrabFrames(ctx, lib.ID)
	if err != nil || n != 1 {
		t.Fatalf("grab: %d %v", n, err)
	}
	items, _, _ := svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 10, 0, library.ItemFilter{})
	if len(items) != 1 || !items[0].HasBackdrop {
		t.Fatalf("backdrop flag: %+v", items)
	}
	if st, err := os.Stat(ImagePath(images, items[0].ID, "backdrop")); err != nil || st.Size() < 1024 {
		t.Fatalf("frame not written: %v", err)
	}
	// Ambient colours come from the new frame.
	if n, err := r.FillPalettes(ctx, lib.ID); err != nil || n != 1 {
		t.Fatalf("palettes: %d %v", n, err)
	}
	items, _, _ = svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 10, 0, library.ItemFilter{})
	if len(items[0].Colors) != 2 || items[0].Colors[0][0] != '#' {
		t.Fatalf("colors: %v", items[0].Colors)
	}
	// Tried once: a second run does nothing.
	if n, _ := r.GrabFrames(ctx, lib.ID); n != 0 {
		t.Fatalf("second grab: %d", n)
	}
}

func TestLocalArtworkIntegration(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg not installed")
	}
	pool := testPool(t)
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "media", "Local Art (2021)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Local Art (2021).mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	// A big poster and a transparent logo, as a user would drop them in.
	for name, src := range map[string]string{"poster.png": "color=c=red:s=2000x3000", "clearlogo.png": "color=c=white@0.5:s=1200x400,format=rgba"} {
		if out, err := exec.Command(ffmpeg, "-v", "error", "-f", "lavfi", "-i", src, "-frames:v", "1", filepath.Join(dir, name)).CombinedOutput(); err != nil {
			t.Fatalf("make %s: %v %s", name, err, out)
		}
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	roots := library.Roots{root}
	svc := &library.Service{Pool: pool, Roots: roots, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	scanner := &library.Scanner{Pool: pool, Roots: roots, Log: log,
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv", DurationMS: 4000}, nil
		}}
	lib, err := svc.Create(ctx, "Local", "movies", []string{filepath.Join(root, "media")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	images := filepath.Join(root, "images")
	r := &Refresher{Pool: pool, ImageDir: images, Log: log, FFmpeg: ffmpeg,
		ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }}
	if n, err := r.LocalArtwork(ctx, lib.ID); err != nil || n != 1 {
		t.Fatalf("import: %d %v", n, err)
	}
	items, _, _ := svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 10, 0, library.ItemFilter{})
	if len(items) != 1 || !items[0].HasPoster || !items[0].HasLogo || items[0].HasBackdrop {
		t.Fatalf("flags: %+v", items)
	}
	poster := ImagePath(images, items[0].ID, "poster")
	data, err := os.ReadFile(poster)
	if err != nil || len(data) < 3 || data[0] != 0xff || data[1] != 0xd8 {
		t.Fatalf("poster should be a JPEG: %v", err)
	}
	logo, _ := os.ReadFile(ImagePath(images, items[0].ID, "logo"))
	if len(logo) < 8 || string(logo[1:4]) != "PNG" {
		t.Fatal("logo should keep its transparency as PNG")
	}
	// Unchanged files are not imported again; downloads never replace them.
	if n, _ := r.LocalArtwork(ctx, lib.ID); n != 0 {
		t.Fatalf("second import: %d", n)
	}
	if !r.saveImage(ctx, nil, items[0].ID, "poster", "/x.jpg", "w500") {
		t.Fatal("local poster should count as present")
	}
	if got, _ := os.ReadFile(poster); string(got) != string(data) {
		t.Fatal("download replaced the user's poster")
	}
}

func TestLocalNFOIntegration(t *testing.T) {
	pool := testPool(t)
	ctx := context.Background()
	root := t.TempDir()
	dir := filepath.Join(root, "media", "Nfo Movie (2010)")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "Nfo Movie (2010).mkv"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	nfo := `<movie><title>Proper Title</title><plot>The plot.</plot><genre>Drama</genre><studio>Blender Institute</studio>
<director>Ana</director><actor><name>Ivo</name><role>Hero</role></actor><set>Open Movies</set><rating>7.5</rating></movie>`
	if err := os.WriteFile(filepath.Join(dir, "movie.nfo"), []byte(nfo), 0o644); err != nil {
		t.Fatal(err)
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	roots := library.Roots{root}
	svc := &library.Service{Pool: pool, Roots: roots, Enqueue: func(context.Context, uuid.UUID) error { return nil }}
	scanner := &library.Scanner{Pool: pool, Roots: roots, Log: log,
		Probe: func(context.Context, string) (library.ProbeResult, error) {
			return library.ProbeResult{Container: "mkv"}, nil
		}}
	lib, err := svc.Create(ctx, "Nfo", "movies", []string{filepath.Join(root, "media")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = svc.Delete(context.Background(), lib.ID) })
	if _, err := scanner.Scan(ctx, lib.ID); err != nil {
		t.Fatal(err)
	}
	r := &Refresher{Pool: pool, Log: log, ResolvePath: func(p string) (string, error) { return library.ResolveSafePath(roots, p) }}
	if n, err := r.LocalNFO(ctx, lib.ID); err != nil || n != 1 {
		t.Fatalf("apply: %d %v", n, err)
	}
	items, _, _ := svc.ListItems(ctx, lib.ID, uuid.Nil, "title", 10, 0, library.ItemFilter{})
	if len(items) != 1 || items[0].Title != "Proper Title" || items[0].Rating == nil || *items[0].Rating != 7.5 {
		t.Fatalf("item: %+v", items)
	}
	m, err := svc.GetMetadata(ctx, items[0].ID)
	if err != nil || m.Overview != "The plot." || len(m.Genres) != 1 || len(m.Studios) != 1 || len(m.Cast) != 1 || m.Crew[0].Name != "Ana" {
		t.Fatalf("metadata: %+v %v", m, err)
	}
	var cols int
	_ = pool.QueryRow(ctx, `SELECT count(*) FROM collection_items ci JOIN collections c ON c.id = ci.collection_id WHERE c.name = 'Open Movies' AND ci.item_id = $1`, items[0].ID).Scan(&cols)
	if cols != 1 {
		t.Fatal("set should become a collection")
	}
	// Unchanged file: nothing to do.
	if n, _ := r.LocalNFO(ctx, lib.ID); n != 0 {
		t.Fatalf("second apply: %d", n)
	}
	_, _ = pool.Exec(ctx, `DELETE FROM collections WHERE name = 'Open Movies'`)
}
