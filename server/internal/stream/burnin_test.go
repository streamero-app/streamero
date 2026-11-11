package stream

import (
	"slices"
	"strings"
	"testing"

	"github.com/streamero-app/streamero/server/internal/library"
)

func burnFile() library.MediaFile {
	return library.MediaFile{Container: "mp4", VideoCodec: "h264", Height: intp(1080), Audio: []library.AudioStream{{Codec: "aac"}},
		Subtitles: []library.SubtitleStream{
			{Codec: "subrip", Language: "eng"},
			{Codec: "hdmv_pgs_subtitle", Language: "hrv"},
			{Codec: "ass", Language: "eng"},
			{Codec: "dvd_subtitle", Language: "eng", Forced: true},
			{Codec: "sup", Language: "deu", External: true, Path: "/m/film.de.sup"},
		}}
}

func TestDecideBurnsInBitmapSubtitles(t *testing.T) {
	web := Device{Containers: []string{"mp4"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, HLS: true}
	f := burnFile()

	if p := DecideWithSubtitle(f, 0, -1, false, web); p.Method != "direct" || p.Burn != nil {
		t.Fatalf("no subtitle: %+v", p)
	}
	// Text tracks are delivered as WebVTT: the picture is untouched.
	if p := DecideWithSubtitle(f, 0, 0, false, web); p.Method != "direct" || p.Burn != nil {
		t.Fatalf("srt: %+v", p)
	}
	// PGS cannot be shown by a web player: transcode with the track drawn in.
	p := DecideWithSubtitle(f, 0, 1, false, web)
	if p.Method != "transcode" || !p.TranscodeVideo || p.TranscodeAudio || p.Burn == nil || !p.Burn.Bitmap ||
		p.Burn.Stream != 1 || p.Burn.Index != 1 || p.Reason != "subtitle burn-in" {
		t.Fatalf("pgs: %+v %+v", p, p.Burn)
	}
	// Stream counts embedded tracks only.
	if p := DecideWithSubtitle(f, 0, 3, false, web); p.Burn == nil || p.Burn.Stream != 3 || p.Burn.Codec != "dvd_subtitle" {
		t.Fatalf("vobsub: %+v", p.Burn)
	}
	if p := DecideWithSubtitle(f, 0, 4, false, web); p.Burn == nil || !p.Burn.External || p.Burn.Stream != -1 || !p.Burn.Bitmap {
		t.Fatalf("external sup: %+v", p.Burn)
	}
	// Burn-in adds to other reasons.
	mkv := f
	mkv.Container = "mkv"
	mkv.Audio = []library.AudioStream{{Codec: "dts"}}
	if p := DecideWithSubtitle(mkv, 0, 1, false, web); p.Method != "transcode" || !p.TranscodeAudio ||
		p.Reason != "container mkv, subtitle burn-in, audio codec dts" {
		t.Fatalf("mkv + dts + pgs: %+v", p)
	}
	// A player that draws PGS itself plays the file directly...
	tv := web
	tv.SubtitleCodecs = []string{"hdmv_pgs_subtitle"}
	if p := DecideWithSubtitle(f, 0, 1, false, tv); p.Method != "direct" || p.Burn != nil {
		t.Fatalf("pgs-capable direct: %+v", p)
	}
	// ...but not from a rebuilt (HLS) stream, nor from a sidecar file.
	if p := DecideWithSubtitle(mkv, 0, 1, false, tv); p.Burn == nil {
		t.Fatalf("pgs-capable remux: %+v", p)
	}
	tv.SubtitleCodecs = append(tv.SubtitleCodecs, "sup")
	if p := DecideWithSubtitle(f, 0, 4, false, tv); p.Burn == nil {
		t.Fatalf("pgs-capable external: %+v", p)
	}
	// ASS burned in on request (keeps its styling); never by default.
	if p := DecideWithSubtitle(f, 0, 2, false, web); p.Burn != nil {
		t.Fatalf("ass by default: %+v", p)
	}
	if p := DecideWithSubtitle(f, 0, 2, true, web); p.Burn == nil || p.Burn.Bitmap || p.Burn.Stream != 2 || p.Method != "transcode" {
		t.Fatalf("ass forced: %+v", p.Burn)
	}
	// Out of range, and music, are ignored.
	if p := DecideWithSubtitle(f, 0, 9, false, web); p.Burn != nil {
		t.Fatalf("out of range: %+v", p)
	}
	song := library.MediaFile{Container: "flac", Audio: []library.AudioStream{{Codec: "flac"}}, Subtitles: f.Subtitles}
	if p := DecideWithSubtitle(song, 0, 1, true, web); p.Burn != nil || p.TranscodeVideo {
		t.Fatalf("audio only: %+v", p)
	}
}

func argAfter(args []string, flag string) string {
	if i := slices.Index(args, flag); i >= 0 && i+1 < len(args) {
		return args[i+1]
	}
	return ""
}

func TestArgsBurnIn(t *testing.T) {
	f := burnFile()
	web := Device{Containers: []string{"mp4"}, VideoCodecs: []string{"h264"}, AudioCodecs: []string{"aac"}, HLS: true}

	// Bitmap overlay: filter graph, the composed picture is mapped.
	j := Job{Input: "/m/film.mkv", AudioIndex: 0, Plan: DecideWithSubtitle(f, 0, 1, false, web)}
	a := Args(j, "/t/s", DefaultEncoding())
	g := argAfter(a, "-filter_complex")
	if !strings.HasPrefix(g, "[0:v:0][0:s:1]overlay=") || !strings.HasSuffix(g, "format=yuv420p[vout]") ||
		argAfter(a, "-map") != "[vout]" || slices.Contains(a, "-vf") || argAfter(a, "-c:v") != "libx264" || argAfter(a, "-c:a") != "copy" {
		t.Fatalf("pgs args: %q", a)
	}
	// HDR: tone mapping before the overlay, scaling after it.
	hdr := f
	hdr.HDR, hdr.Height = "HDR10", intp(2160)
	small := web
	small.MaxHeight = 720
	j.Plan = DecideWithSubtitle(hdr, 0, 1, false, small)
	g = argAfter(Args(j, "/t/s", DefaultEncoding()), "-filter_complex")
	if !strings.HasPrefix(g, "[0:v:0]zscale=") || !strings.Contains(g, "[base];[base][0:s:1]overlay=") ||
		strings.Index(g, "overlay") > strings.Index(g, "scale=-2:720") {
		t.Fatalf("hdr graph: %s", g)
	}
	// GPU encoders: the overlay runs before the upload, the hardware encoder stays.
	for hw, want := range map[string]string{"vaapi": "hwupload[vout]", "qsv": "hwupload=extra_hw_frames=64[vout]", "nvenc": "format=yuv420p[vout]"} {
		enc := DefaultEncoding()
		enc.HWAccel = hw
		j.Plan = DecideWithSubtitle(f, 0, 1, false, web)
		a := Args(j, "/t/s", enc)
		if g := argAfter(a, "-filter_complex"); !strings.HasSuffix(g, want) || !strings.Contains(argAfter(a, "-c:v"), hw) {
			t.Fatalf("%s: %q", hw, a)
		}
	}
	// Sidecar bitmap file: second input, read from its start and moved to the stream's clock.
	j = Job{Input: "/m/film.mkv", AudioIndex: 0, StartMS: 90_500, Plan: DecideWithSubtitle(f, 0, 4, false, web)}
	j.Plan.Burn.Path = "/m/film.de.sup"
	a = Args(j, "/t/s", DefaultEncoding())
	s := strings.Join(a, " ")
	if !strings.Contains(s, "-ss 90.500 -i /m/film.mkv -itsoffset -90.500 -i /m/film.de.sup") ||
		!strings.HasPrefix(argAfter(a, "-filter_complex"), "[0:v:0][1:s:0]overlay") {
		t.Fatalf("external sup: %s", s)
	}
	j.StartMS = 0
	if g := argAfter(Args(j, "/t/s", DefaultEncoding()), "-filter_complex"); !strings.HasPrefix(g, "[0:v:0][1:s:0]overlay") {
		t.Fatalf("external sup at 0: %s", g)
	}
	// Text track with libass, shifted to the file's clock when seeking.
	j = Job{Input: "/m/film.mkv", AudioIndex: 0, StartMS: 2_000, Plan: DecideWithSubtitle(f, 0, 2, true, web)}
	vf := argAfter(Args(j, "/t/s", DefaultEncoding()), "-vf")
	if vf != `setpts=PTS+2.000/TB,subtitles=filename=/t/s/burn.mkv:si=2,setpts=PTS-2.000/TB,format=yuv420p` {
		t.Fatalf("ass vf: %s", vf)
	}
	j.StartMS = 0
	if vf := argAfter(Args(j, "/t/s", DefaultEncoding()), "-vf"); vf != `subtitles=filename=/t/s/burn.mkv:si=2,format=yuv420p` {
		t.Fatalf("ass vf at 0: %s", vf)
	}
	// Direct stream copies keep no burn-in.
	j.Plan = Decide(f, 0, web)
	if a := Args(j, "/t/s", DefaultEncoding()); slices.Contains(a, "-filter_complex") || argAfter(a, "-map") != "0:v:0" {
		t.Fatalf("no burn: %q", a)
	}
}

func TestFilterValueEscapes(t *testing.T) {
	if got := filterValue(`/a b/it's: [x],y;z\w`); got != `/a b/it\\\'s\\: \[x\]\,y\;z\\\\w` {
		t.Fatalf("escaped: %s", got)
	}
	if got := filterValue("/tmp/abc/burn.ass"); got != "/tmp/abc/burn.ass" {
		t.Fatal(got)
	}
}
