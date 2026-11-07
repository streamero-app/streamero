package stream

import (
	"net/url"
	"testing"
	"time"

	"github.com/streamero-app/streamero/server/internal/library"
)

func TestSigner(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	s := Signer{Key: []byte("k"), Now: func() time.Time { return now }}
	q, _ := url.ParseQuery(s.Query("/stream/hls/abc/", time.Minute))
	if !s.Valid("/stream/hls/abc/", q) {
		t.Fatal("fresh signature rejected")
	}
	if s.Valid("/stream/hls/abd/", q) {
		t.Fatal("signature valid for another scope")
	}
	other := Signer{Key: []byte("other"), Now: s.Now}
	if other.Valid("/stream/hls/abc/", q) {
		t.Fatal("signature valid with another key")
	}
	q.Set("exp", "9999999999")
	if s.Valid("/stream/hls/abc/", q) {
		t.Fatal("tampered expiry accepted")
	}
	q, _ = url.ParseQuery(s.Query("/stream/file/x/", time.Minute))
	now = now.Add(2 * time.Minute)
	if s.Valid("/stream/file/x/", q) {
		t.Fatal("expired signature accepted")
	}
	if ScopeOf("/stream/hls/abc/seg_00001.m4s") != "/stream/hls/abc/" || ScopeOf("/stream/x") != "" {
		t.Fatal("scope")
	}
}

func intp(v int) *int { return &v }

func TestDecide(t *testing.T) {
	web := Device{Containers: []string{"mp4", "webm"}, VideoCodecs: []string{"h264", "vp9", "av1"}, AudioCodecs: []string{"aac", "mp3", "opus"}, HLS: true}
	mp4 := library.MediaFile{Container: "mp4", VideoCodec: "h264", Height: intp(1080), Audio: []library.AudioStream{{Codec: "aac"}}}
	if p := Decide(mp4, 0, web); p.Method != "direct" {
		t.Fatalf("mp4/h264/aac: %+v", p)
	}
	mkv := mp4
	mkv.Container = "mkv"
	if p := Decide(mkv, 0, web); p.Method != "remux" || p.TranscodeVideo || p.TranscodeAudio || p.Reason != "container mkv" {
		t.Fatalf("mkv remux: %+v", p)
	}
	dts := mkv
	dts.Audio = []library.AudioStream{{Codec: "dts"}}
	if p := Decide(dts, 0, web); p.Method != "remux" || !p.TranscodeAudio {
		t.Fatalf("dts audio: %+v", p)
	}
	hevc := library.MediaFile{Container: "mkv", VideoCodec: "hevc", Height: intp(2160), HDR: "HDR10", Audio: []library.AudioStream{{Codec: "eac3"}}}
	if p := Decide(hevc, 0, web); p.Method != "transcode" || !p.TranscodeVideo || !p.ToneMap || !p.TranscodeAudio {
		t.Fatalf("hevc hdr: %+v", p)
	}
	small := web
	small.MaxHeight = 720
	if p := Decide(mp4, 0, small); p.Method != "transcode" || p.Height != 720 {
		t.Fatalf("downscale: %+v", p)
	}
	twoTracks := mp4
	twoTracks.Audio = append(twoTracks.Audio, library.AudioStream{Codec: "aac"})
	if p := Decide(twoTracks, 1, web); p.Method != "remux" || p.TranscodeAudio {
		t.Fatalf("second audio track needs remux: %+v", p)
	}
	apple := Device{Containers: []string{"mp4", "mov"}, VideoCodecs: []string{"h264", "hevc", "hdr"}, AudioCodecs: []string{"aac", "ac3", "eac3"}, HLS: true}
	hevcMP4 := hevc
	hevcMP4.Container = "mp4"
	if p := Decide(hevcMP4, 0, apple); p.Method != "direct" {
		t.Fatalf("apple hevc hdr direct: %+v", p)
	}
}
