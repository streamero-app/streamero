package app

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/library"
)

// TestCraftedPlaylistFile: a "video" in the media folder that is really an HLS
// playlist naming a URL and a file outside the folder. Probing or converting it
// must neither make the HTTP request nor read the outside file (D-053). The
// HLS demuxer is forced, as an older ffmpeg (or another build) would pick it
// from the content: the protocol whitelist alone must stop it.
func TestCraftedPlaylistFile(t *testing.T) {
	ffmpeg, err1 := exec.LookPath("ffmpeg")
	ffprobe, err2 := exec.LookPath("ffprobe")
	if err1 != nil || err2 != nil {
		t.Skip("ffmpeg/ffprobe not installed")
	}
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	outside := filepath.Join(t.TempDir(), "secret.ts")
	if err := os.WriteFile(outside, []byte("SECRET OUTSIDE THE MEDIA FOLDER"), 0o600); err != nil {
		t.Fatal(err)
	}
	media := t.TempDir()
	crafted := filepath.Join(media, "film.mkv")
	playlist := "#EXTM3U\n#EXT-X-TARGETDURATION:10\n#EXTINF:10,\n" + srv.URL + "/seg.ts\n#EXTINF:10,\nfile://" + outside +
		"\n#EXTINF:10,\n" + outside + "\n#EXT-X-ENDLIST\n"
	if err := os.WriteFile(crafted, []byte(playlist), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	// The scanner's probe.
	_, _ = library.FFprobe(ffprobe)(ctx, crafted)

	run := func(args ...string) []byte {
		out, _ := exec.CommandContext(ctx, ffmpeg, args...).CombinedOutput() //nolint:gosec // test binary and files
		return out
	}
	outFile := filepath.Join(t.TempDir(), "out.ts")
	// Forced HLS demuxer with the whitelist every library input gets.
	run(append(library.SafeInputArgs(), "-nostdin", "-v", "error", "-f", "hls", "-i", crafted, "-c", "copy", "-y", outFile)...)
	run(append(library.SafeInputArgs(), "-nostdin", "-v", "error", "-i", crafted, "-c", "copy", "-y", outFile)...)
	if n := hits.Load(); n != 0 {
		t.Errorf("ffmpeg made %d HTTP requests for a crafted media file", n)
	}
	if b, err := os.ReadFile(outFile); err == nil && bytes.Contains(b, []byte("SECRET OUTSIDE")) {
		t.Error("the file outside the media folder ended up in the output")
	}
	// The whitelist itself: an http input is refused with it and fetched
	// without it (so this test can see a request when one is made).
	run(append(library.SafeInputArgs(), "-nostdin", "-v", "error", "-i", srv.URL+"/direct.ts", "-f", "null", "-")...)
	if n := hits.Load(); n != 0 {
		t.Errorf("an http input passed the file-only whitelist (%d requests)", n)
	}
	run("-nostdin", "-v", "error", "-i", srv.URL+"/direct.ts", "-f", "null", "-")
	if hits.Load() == 0 {
		t.Error("control: ffmpeg made no request without the whitelist; the test cannot see requests")
	}
}
