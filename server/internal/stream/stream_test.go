package stream

import (
	"context"
	"errors"
	"net/url"
	"strings"
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

func TestArgsFollowEncodingSettings(t *testing.T) {
	j := Job{Input: "/m/a.mkv", StartMS: 1500, AudioIndex: 0, Plan: Plan{TranscodeVideo: true, TranscodeAudio: true, Height: 720, ToneMap: true}}
	has := func(a []string, seq ...string) bool {
		for i := 0; i+len(seq) <= len(a); i++ {
			ok := true
			for k, s := range seq {
				if a[i+k] != s {
					ok = false
					break
				}
			}
			if ok {
				return true
			}
		}
		return false
	}
	def := Args(j, "/t", Encoding{})
	if !has(def, "-c:v", "libx264", "-preset", "veryfast", "-crf", "22") || !has(def, "-b:a", "192k") || !has(def, "-hls_time", "6") {
		t.Fatalf("defaults: %v", def)
	}
	if !strings.Contains(strings.Join(def, " "), "tonemap=tonemap=hable") {
		t.Fatal("tone mapping should be on by default")
	}
	nv := Args(j, "/t", Encoding{HWAccel: "nvenc", Preset: "fast", Quality: 19, AudioKbps: 256, SegmentSeconds: 4})
	if !has(nv, "-c:v", "h264_nvenc", "-preset", "p5") || !has(nv, "-cq", "19") || !has(nv, "-b:a", "256k") || !has(nv, "-hls_time", "4") || !has(nv, "-g", "96") {
		t.Fatalf("nvenc: %v", nv)
	}
	off := false
	va := Args(j, "/t", Encoding{HWAccel: "vaapi", VAAPIDevice: "/dev/dri/renderD129", ToneMapping: &off})
	if !has(va, "-vaapi_device", "/dev/dri/renderD129") || !has(va, "-c:v", "h264_vaapi") {
		t.Fatalf("vaapi: %v", va)
	}
	if va[len(va)-1] != "/t/index.m3u8" || strings.Contains(strings.Join(va, " "), "tonemap") {
		t.Fatalf("vaapi args: %v", va)
	}
	// Hardware options come before the input; the seek stays an input option.
	iDev, iIn := -1, -1
	for i, s := range va {
		if s == "-vaapi_device" {
			iDev = i
		}
		if s == "-i" {
			iIn = i
		}
	}
	if iDev < 0 || iDev > iIn {
		t.Fatal("hardware device must be set before the input")
	}
}

func TestManagerRefusesBeyondMaxSessions(t *testing.T) {
	m := &Manager{Encoding: func(context.Context) Encoding { return Encoding{MaxSessions: 1} }, streams: map[string]*session{"x": {}}}
	if _, err := m.Start(context.Background(), Job{Plan: Plan{TranscodeVideo: true}}); !errors.Is(err, ErrBusy) {
		t.Fatalf("want ErrBusy, got %v", err)
	}
}

func TestDecideRespectsBitrateLimit(t *testing.T) {
	dur := int64(600_000) // 10 min
	h := 1080
	f := library.MediaFile{Container: "mp4", VideoCodec: "h264", Size: 750_000_000, DurationMS: &dur, Height: &h, // ~10 Mbps
		Audio: []library.AudioStream{{Codec: "aac"}}}
	d := Device{Containers: []string{"mp4"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, HLS: true}
	if p := Decide(f, 0, d); p.Method != "direct" {
		t.Fatalf("no limit: %+v", p)
	}
	d.MaxBitrate = 3_000_000
	p := Decide(f, 0, d)
	if p.Method != "transcode" || p.MaxBitrate != 2_744_000 || p.Height != 720 || !strings.Contains(p.Reason, "bitrate") {
		t.Fatalf("limited: %+v", p)
	}
	if a := Args(Job{Input: "x", Plan: p, AudioIndex: 0}, "/t", Encoding{}); !strings.Contains(strings.Join(a, " "), "-maxrate 2744000 -bufsize 5488000") {
		t.Fatalf("args: %v", a)
	}
	d.MaxBitrate = 20_000_000
	if p := Decide(f, 0, d); p.Method != "direct" {
		t.Fatalf("under the limit: %+v", p)
	}
}

func TestAudioOnlyTranscodeHasNoVideo(t *testing.T) {
	dur := int64(200_000)
	f := library.MediaFile{Container: "flac", Size: 30_000_000, DurationMS: &dur, Audio: []library.AudioStream{{Codec: "alac", Channels: 2}}}
	p := Decide(f, 0, Device{Containers: []string{"mp4", "flac"}, AudioCodecs: []string{"aac", "flac"}, HLS: true})
	if p.Method != "remux" || !p.AudioOnly || p.TranscodeVideo || !p.TranscodeAudio {
		t.Fatalf("plan: %+v", p)
	}
	a := strings.Join(Args(Job{Input: "x.m4a", AudioIndex: 0, Plan: p}, "/t", Encoding{}), " ")
	if strings.Contains(a, "0:v:0") || !strings.Contains(a, "-vn") || !strings.Contains(a, "-c:a aac") {
		t.Fatalf("args: %s", a)
	}
	if d := Decide(library.MediaFile{Container: "flac", Audio: []library.AudioStream{{Codec: "flac"}}}, 0, Device{Containers: []string{"flac"}, AudioCodecs: []string{"flac"}}); d.Method != "direct" {
		t.Fatalf("flac in a browser that plays it: %+v", d)
	}
}
