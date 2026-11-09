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
