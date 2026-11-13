package stream

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// fakeFFmpeg writes a one-segment playlist to its last argument and keeps
// running like a real transcode; with a hardware encoder in its arguments it fails.
func fakeFFmpeg(t *testing.T) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "ffmpeg")
	script := `#!/bin/sh
for a in "$@"; do
  case "$a" in h264_vaapi) echo "no VA-API device" >&2; exit 1;; esac
  last="$a"
done
printf '#EXTM3U\n#EXT-X-VERSION:7\n#EXT-X-MAP:URI="init.mp4"\n#EXTINF:6.0,\nseg_00000.m4s\n' > "$last"
exec sleep 60
`
	if err := os.WriteFile(p, []byte(script), 0o700); err != nil { // #nosec G306 -- test helper
		t.Fatal(err)
	}
	return p
}

func testManager(t *testing.T, enc Encoding) *Manager {
	t.Helper()
	m := &Manager{Dir: t.TempDir(), FFmpeg: fakeFFmpeg(t), Log: slog.New(slog.NewTextHandler(io.Discard, nil)),
		Encoding: func(context.Context) Encoding { return enc }}
	t.Cleanup(m.StopAll)
	return m
}

func TestNewStreamReplacesTheDevicesPrevious(t *testing.T) {
	m := testManager(t, Encoding{MaxSessions: 1})
	ctx := context.Background()
	job := Job{Input: "/m/a.mkv", Plan: Plan{Method: "remux"}, Client: "session-a"}
	first, err := m.Start(ctx, job)
	if err != nil {
		t.Fatal(err)
	}
	// A seek or audio switch restarts the stream: with a cap of one it must still work.
	job.StartMS = 60_000
	second, err := m.Start(ctx, job)
	if err != nil {
		t.Fatalf("restart on the same device: %v", err)
	}
	if m.Alive(first) || !m.Alive(second) || m.Active() != 1 {
		t.Fatalf("first alive=%v second alive=%v active=%d", m.Alive(first), m.Alive(second), m.Active())
	}
	if _, err := os.Stat(filepath.Join(m.Dir, first)); !os.IsNotExist(err) {
		t.Fatal("the replaced session's directory was not removed")
	}
	// Another device does not replace it (and here hits the cap of one).
	if _, err := m.Start(ctx, Job{Input: "/m/b.mkv", Plan: Plan{Method: "remux"}, Client: "session-b"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("second device over the cap: %v", err)
	}
}

func TestPerAccountSessionCap(t *testing.T) {
	m := testManager(t, Encoding{MaxSessions: 20})
	ctx := context.Background()
	alice, bob := uuid.New(), uuid.New()
	for i := range PerUserSessions {
		if _, err := m.Start(ctx, Job{Plan: Plan{Method: "remux"}, Owner: alice, Client: "a" + string(rune('0'+i))}); err != nil {
			t.Fatalf("stream %d: %v", i, err)
		}
	}
	if _, err := m.Start(ctx, Job{Plan: Plan{Method: "remux"}, Owner: alice, Client: "a-extra"}); !errors.Is(err, ErrBusy) {
		t.Fatalf("over the account cap: %v", err)
	}
	// Seeking on a device the account already uses is still fine.
	if _, err := m.Start(ctx, Job{Plan: Plan{Method: "remux"}, Owner: alice, Client: "a0", StartMS: 5000}); err != nil {
		t.Fatalf("restart at the account cap: %v", err)
	}
	if _, err := m.Start(ctx, Job{Plan: Plan{Method: "remux"}, Owner: bob, Client: "b0"}); err != nil {
		t.Fatalf("another account: %v", err)
	}
	if _, err := m.Start(ctx, Job{Plan: Plan{Method: "remux"}}); err != nil { // administrators, live channels
		t.Fatalf("uncounted session: %v", err)
	}
}

func TestServerCapCountsRemuxAndHasADefault(t *testing.T) {
	if (Encoding{}).SessionLimit() < 4 || (Encoding{MaxSessions: 2}).SessionLimit() != 2 {
		t.Fatal("session limit defaults")
	}
	m := testManager(t, Encoding{MaxSessions: 1})
	if _, err := m.Start(context.Background(), Job{Plan: Plan{Method: "remux"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Start(context.Background(), Job{Plan: Plan{Method: "remux"}}); !errors.Is(err, ErrBusy) {
		t.Fatalf("remux over the server cap: %v", err)
	}
}

func TestHardwareFailureFallsBackToSoftware(t *testing.T) {
	m := testManager(t, Encoding{HWAccel: "vaapi"})
	id, err := m.Start(context.Background(), Job{Plan: Plan{Method: "transcode", TranscodeVideo: true}, AudioIndex: -1})
	if err != nil || !m.Alive(id) || m.Active() != 1 {
		t.Fatalf("fallback: id=%q err=%v active=%d", id, err, m.Active())
	}
}

func TestPlaylistStartsFilmsAtTheBeginning(t *testing.T) {
	m := testManager(t, Encoding{})
	id, err := m.Start(context.Background(), Job{Plan: Plan{Method: "remux"}})
	if err != nil {
		t.Fatal(err)
	}
	pl, err := m.Playlist(id, "exp=1&sig=x")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(pl), "#EXTM3U\n#EXT-X-START:TIME-OFFSET=0,PRECISE=YES\n") || !strings.Contains(string(pl), "seg_00000.m4s?exp=1&sig=x") {
		t.Fatalf("playlist:\n%s", pl)
	}
	live, err := m.StartLive(context.Background(), Job{Plan: Plan{Method: "remux"}}, 10, nil)
	if err != nil {
		t.Fatal(err)
	}
	if pl, _ := m.Playlist(live, "q"); strings.Contains(string(pl), "EXT-X-START") {
		t.Fatal("a live window must not start at its oldest segment")
	}
}

func TestCopiedHEVCIsTaggedHvc1AndVODIsThrottled(t *testing.T) {
	a := Args(Job{Input: "/m/a.mkv", HEVC: true, Plan: Plan{Method: "remux"}}, "/t", Encoding{})
	if !slices.Contains(a, "hvc1") || !slices.Contains(a, "-readrate") {
		t.Fatalf("remux args: %v", a)
	}
	if i, in := slices.Index(a, "-readrate"), slices.Index(a, "-i"); i > in {
		t.Fatal("-readrate must be an input option")
	}
	if a := Args(Job{Input: "/m/a.mkv", HEVC: true, Plan: Plan{Method: "transcode", TranscodeVideo: true}}, "/t", Encoding{}); slices.Contains(a, "hvc1") {
		t.Fatal("a transcode to H.264 must not be tagged hvc1")
	}
	if l := LiveArgs(Job{Input: "http://tuner/1", Plan: Plan{Method: "remux"}}, "/t", Encoding{}, 10); slices.Contains(l, "-readrate") {
		t.Fatal("live input already arrives in real time")
	}
}

func TestCleanStaleRemovesOnlySessionDirectories(t *testing.T) {
	m := &Manager{Dir: t.TempDir()}
	stale := filepath.Join(m.Dir, newID())
	other := filepath.Join(m.Dir, "keep-me")
	for _, d := range []string{stale, other} {
		if err := os.MkdirAll(d, 0o750); err != nil {
			t.Fatal(err)
		}
	}
	m.CleanStale()
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Fatal("stale session directory kept")
	}
	if _, err := os.Stat(other); err != nil {
		t.Fatal("unrelated directory removed")
	}
}

func TestSignerRefusesFarFutureExpiry(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := Signer{Key: []byte("k"), Now: func() time.Time { return now }}
	ok, _ := url.ParseQuery(s.Query("/stream/file/x/", 48*time.Hour))
	if !s.Valid("/stream/file/x/", ok) {
		t.Fatal("the longest TTL the server issues was refused")
	}
	forever, _ := url.ParseQuery(s.Query("/stream/file/x/", 10*365*24*time.Hour))
	if s.Valid("/stream/file/x/", forever) {
		t.Fatal("a signature valid for years was accepted")
	}
	if s.ValidToken("/stream/file/x/", s.Token("/stream/file/x/", 1000*time.Hour)) {
		t.Fatal("a far-future token was accepted")
	}
}
