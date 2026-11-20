package stream

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
)

func testFileForWhitelist() library.MediaFile {
	h := 1080
	return library.MediaFile{Path: "/m/a.mkv", Container: "mkv", VideoCodec: "hevc", Height: &h,
		Audio: []library.AudioStream{{Codec: "aac", Channels: 2}}}
}

func TestSignerGrantBinding(t *testing.T) {
	revoked := map[string]bool{}
	s := Signer{Key: []byte("k"), Grant: func(_ context.Context, g string) bool { return !revoked[g] }}
	ctx := context.Background()
	scope := "/stream/file/abc/"

	q, _ := url.ParseQuery(s.QueryFor(scope, "s_1_main", time.Hour))
	if q.Get("g") != "s_1_main" || !s.ValidFor(ctx, scope, q) {
		t.Fatalf("bound link refused: %v", q)
	}
	if s.Valid(scope, q) {
		t.Error("a bound signature validated without its grant")
	}
	if s.ValidFor(ctx, "/stream/file/other/", q) {
		t.Error("bound link valid for another scope")
	}
	other := url.Values{"exp": q["exp"], "sig": q["sig"], "g": {"s_2_main"}}
	if s.ValidFor(ctx, scope, other) {
		t.Error("grant swapped without a new signature")
	}
	stripped := url.Values{"exp": q["exp"], "sig": q["sig"]}
	if s.ValidFor(ctx, scope, stripped) {
		t.Error("grant removed from a bound link still valid")
	}
	revoked["s_1_main"] = true
	if s.ValidFor(ctx, scope, q) {
		t.Error("link still valid after its grant was revoked")
	}

	// Unbound links (DLNA, Live TV images, downloads) keep working.
	u, _ := url.ParseQuery(s.QueryFor(scope, "", time.Hour))
	if u.Get("g") != "" || !s.ValidFor(ctx, scope, u) {
		t.Errorf("unbound link refused: %v", u)
	}

	// Tokens (the compat media source tag).
	tok := s.TokenFor(scope, "k_9", time.Hour)
	if strings.Count(tok, ".") != 2 || !s.ValidTokenFor(ctx, scope, tok) {
		t.Fatalf("token %q refused", tok)
	}
	if s.ValidTokenFor(ctx, scope, strings.TrimSuffix(tok, "k_9")+"k_8") || s.ValidTokenFor(ctx, scope, tok+".") {
		t.Error("altered token accepted")
	}
	revoked["k_9"] = true
	if s.ValidTokenFor(ctx, scope, tok) {
		t.Error("token valid after revocation")
	}
	if !s.ValidTokenFor(ctx, scope, s.Token(scope, time.Hour)) {
		t.Error("unbound token refused")
	}
	if got := SignatureQuery(q); !strings.Contains(got, "g=s_1_main") || !strings.Contains(got, "sig=") {
		t.Errorf("signature query %q", got)
	}
}

func TestSubtitleCacheLimitsAndCaches(t *testing.T) {
	var c subtitleCache
	var running, peak, calls atomic.Int32
	conv := func(context.Context) ([]byte, error) {
		calls.Add(1)
		n := running.Add(1)
		for {
			p := peak.Load()
			if n <= p || peak.CompareAndSwap(p, n) {
				break
			}
		}
		time.Sleep(30 * time.Millisecond)
		running.Add(-1)
		return []byte("WEBVTT"), nil
	}
	var wg sync.WaitGroup
	// 40 requests for 20 different tracks, two each at the same time.
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			key := subtitleKey(uuid.Nil, i%20, "webvtt", "/nonexistent")
			if out, err := c.convert(context.Background(), key, conv); err != nil || string(out) != "WEBVTT" {
				t.Errorf("convert: %q %v", out, err)
			}
		}()
	}
	wg.Wait()
	if p := peak.Load(); p > subtitleWorkers {
		t.Errorf("%d conversions ran at once, limit %d", p, subtitleWorkers)
	}
	if n := calls.Load(); n > 20 {
		t.Errorf("%d conversions for 20 tracks: identical requests were not shared", n)
	}
	before := calls.Load()
	if _, err := c.convert(context.Background(), subtitleKey(uuid.Nil, 3, "webvtt", "/nonexistent"), conv); err != nil {
		t.Fatal(err)
	}
	if calls.Load() != before {
		t.Error("a converted track was converted again")
	}
	// Failures are not cached.
	fail := func(context.Context) ([]byte, error) { return nil, errors.New("bad") }
	key := subtitleKey(uuid.Nil, 99, "webvtt", "/nonexistent")
	if _, err := c.convert(context.Background(), key, fail); err == nil {
		t.Fatal("error lost")
	}
	if _, ok := c.get(key); ok {
		t.Error("failure cached")
	}
}

func TestLibraryInputsGetProtocolWhitelist(t *testing.T) {
	f := testFileForWhitelist()
	for name, args := range map[string][]string{
		"hls":         Args(Job{Input: "/m/a.mkv", AudioIndex: 0, Plan: Decide(f, 0, Device{HLS: true, VideoCodecs: []string{"h264"}})}, "/t/s", DefaultEncoding()),
		"progressive": ProgressiveArgs(Job{Input: "/m/a.mkv", AudioIndex: 0, Plan: Decide(f, 0, Device{})}, DefaultEncoding(), "mpegts"),
	} {
		s := strings.Join(args, " ")
		if !strings.Contains(s, "-protocol_whitelist file") || strings.Index(s, "-protocol_whitelist") > strings.Index(s, "-i /m/a.mkv") {
			t.Errorf("%s: no file-only whitelist before the input: %s", name, s)
		}
	}
	// A URL input (Live TV) keeps the job's own options.
	live := Args(Job{Input: "http://tuner/1", InputArgs: []string{"-protocol_whitelist", "http"}, AudioIndex: 0,
		Plan: Decide(f, 0, Device{HLS: true, VideoCodecs: []string{"h264"}})}, "/t/s", DefaultEncoding())
	if s := strings.Join(live, " "); strings.Contains(s, "-protocol_whitelist file") {
		t.Errorf("live input got the file whitelist: %s", s)
	}
}
