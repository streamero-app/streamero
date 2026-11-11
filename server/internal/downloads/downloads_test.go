package downloads

import (
	"slices"
	"strings"
	"testing"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

func TestPickEpisodes(t *testing.T) {
	ids := make([]uuid.UUID, 6)
	for i := range ids {
		ids[i] = uuid.New()
	}
	eps := []Episode{{ids[0], true}, {ids[1], true}, {ids[2], false}, {ids[3], true}, {ids[4], false}, {ids[5], false}}

	if got := pickEpisodes(eps, 0, nil); !slices.Equal(got, ids) {
		t.Fatalf("whole season: %v", got)
	}
	// Next 2 after the last watched episode (ep 4 was watched out of order).
	if got := pickEpisodes(eps, 2, nil); !slices.Equal(got, ids[4:6]) {
		t.Fatalf("next 2: %v", got)
	}
	// Episodes already on the device count towards N but are not downloaded again.
	if got := pickEpisodes(eps, 2, []uuid.UUID{ids[4]}); !slices.Equal(got, ids[5:6]) {
		t.Fatalf("next 2 with one on the device: %v", got)
	}
	if got := pickEpisodes(eps[:2], 3, nil); len(got) != 0 {
		t.Fatalf("all watched: %v", got)
	}
}

func TestSettingsNormalize(t *testing.T) {
	if s := (Settings{}).Normalize(); s != DefaultSettings() {
		t.Fatalf("defaults: %+v", s)
	}
	if s := (Settings{MaxTranscodes: 3, CacheLimitGB: 10, KeepDays: 2}).Normalize(); s.MaxTranscodes != 3 || s.CacheLimitGB != 10 || s.KeepDays != 2 {
		t.Fatalf("kept: %+v", s)
	}
}

func TestDownloadPlanAndArgs(t *testing.T) {
	h1080, h2160, h720 := 1080, 2160, 720
	dur := int64(60_000)
	aac := []library.AudioStream{{Codec: "aac", Channels: 2}}
	// 1080p H.264 at ~4 Mbit/s with stereo AAC: fits 1080p, copied as is.
	f := library.MediaFile{VideoCodec: "h264", Height: &h1080, DurationMS: &dur, Size: 30_000_000, Audio: aac}
	p := stream.DownloadPlan(f, 0, Presets[Q1080].Height, Presets[Q1080].Bitrate)
	if p.TranscodeVideo || p.TranscodeAudio {
		t.Fatalf("fitting file converted: %+v", p)
	}
	// The same file for 720p: scaled and capped.
	p = stream.DownloadPlan(f, 0, Presets[Q720].Height, Presets[Q720].Bitrate)
	if !p.TranscodeVideo || p.Height != 720 || p.MaxBitrate != 2_000_000 {
		t.Fatalf("720p: %+v", p)
	}
	// 4K HEVC HDR with 5.1 E-AC-3 at source quality: converted, tone mapped, no scaling.
	g := library.MediaFile{VideoCodec: "hevc", HDR: "HDR10", Height: &h2160, DurationMS: &dur, Size: 100_000_000,
		Audio: []library.AudioStream{{Codec: "eac3", Channels: 6}}}
	p = stream.DownloadPlan(g, 0, Presets[Source].Height, Presets[Source].Bitrate)
	if !p.TranscodeVideo || !p.ToneMap || p.Height != 0 || p.MaxBitrate != 0 || !p.TranscodeAudio {
		t.Fatalf("source: %+v", p)
	}
	// 10-bit H.264 does not play on phones' decoders.
	ten := library.MediaFile{VideoCodec: "h264", VideoBitDepth: 10, Height: &h720, Audio: aac}
	if p := stream.DownloadPlan(ten, 0, 0, 0); !p.TranscodeVideo {
		t.Fatalf("10-bit copied: %+v", p)
	}
	args := strings.Join(stream.DownloadArgs(stream.Job{Input: "/m/in.mkv", AudioIndex: 0,
		Plan: stream.DownloadPlan(g, 0, 720, 2_000_000)}, stream.DefaultEncoding(), "/c/x.part.mp4"), " ")
	for _, want := range []string{"-i /m/in.mkv", "libx264", "scale=-2:720", "-maxrate 2000000", "-c:a aac", "-ac 2", "-sn",
		"+faststart", "-progress pipe:1", "-f mp4 -y /c/x.part.mp4", "tonemap"} {
		if !strings.Contains(args, want) {
			t.Fatalf("args miss %q: %s", want, args)
		}
	}
}
