package stream

import (
	"fmt"
	"slices"
	"strings"

	"github.com/streamero-app/streamero/server/internal/library"
)

// Device is what a client can play natively.
type Device struct {
	Containers, VideoCodecs, AudioCodecs []string
	HLS                                  bool
	MaxHeight, MaxBitrate                int
	// SubtitleCodecs are the bitmap subtitle formats the client draws itself when it
	// plays a file directly; any other bitmap track is burned into the picture.
	SubtitleCodecs []string
}

// BurnIn is a subtitle track drawn into the picture by the transcoder.
type BurnIn struct {
	Index    int    // position in the file's subtitle list
	Stream   int    // embedded: index among the file's subtitle streams (0:s:N); -1 for a sidecar file
	Path     string // sidecar: the validated file to read (filled in by Server.Start)
	Codec    string
	Bitmap   bool // overlay a bitmap track; otherwise libass renders text (ASS/SSA keep their styling)
	External bool
}

// IsBitmapSubtitle reports whether a subtitle codec (ffprobe name or sidecar
// extension) is a picture format: PGS, VobSub, DVB, XSUB.
func IsBitmapSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "hdmv_pgs_subtitle", "pgssub", "pgs", "sup", "dvd_subtitle", "dvdsub", "vobsub", "sub", "idx",
		"dvb_subtitle", "dvbsub", "xsub":
		return true
	}
	return false
}

// IsTextSubtitle reports whether a subtitle codec is text the server can convert to WebVTT.
func IsTextSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "subrip", "srt", "ass", "ssa", "webvtt", "vtt", "mov_text", "text":
		return true
	}
	return false
}

// burnIn says whether subtitle track sub must be drawn into the picture: bitmap
// tracks unless the client draws them itself from a directly played file, text
// tracks only when asked (force), e.g. to keep ASS styling.
func burnIn(f library.MediaFile, sub int, force, direct bool, d Device) *BurnIn {
	if sub < 0 || sub >= len(f.Subtitles) || f.VideoCodec == "" {
		return nil
	}
	s := f.Subtitles[sub]
	bitmap := IsBitmapSubtitle(s.Codec)
	switch {
	case bitmap:
		if !force && direct && !s.External && has(d.SubtitleCodecs, s.Codec) {
			return nil
		}
	case IsTextSubtitle(s.Codec):
		if !force {
			return nil
		}
	default:
		return nil
	}
	b := &BurnIn{Index: sub, Stream: -1, Codec: s.Codec, Bitmap: bitmap, External: s.External}
	if !s.External {
		b.Stream = 0
		for _, x := range f.Subtitles[:sub] {
			if !x.External {
				b.Stream++
			}
		}
	}
	return b
}

// Plan says how a file will be delivered.
type Plan struct {
	Method         string // direct | remux | transcode
	Reason         string
	TranscodeVideo bool
	TranscodeAudio bool
	Height         int     // target height when scaling (0 = keep)
	ToneMap        bool    // HDR → SDR
	MaxBitrate     int     // video bits per second when the stream must stay under a limit (0 = quality only)
	AudioOnly      bool    // music: no picture (embedded cover art is left out)
	Burn           *BurnIn // subtitle drawn into the picture (forces a video transcode)
}

// fileBitrate estimates a file's overall bits per second from its size and length.
func fileBitrate(f library.MediaFile) int {
	if f.DurationMS == nil || *f.DurationMS <= 0 || f.Size <= 0 {
		return 0
	}
	return int(f.Size * 8 * 1000 / *f.DurationMS)
}

// heightFor picks a picture height that looks good at a bit rate.
func heightFor(bps int) int {
	switch {
	case bps >= 4_000_000:
		return 1080
	case bps >= 2_000_000:
		return 720
	case bps >= 1_000_000:
		return 480
	default:
		return 360
	}
}

func has(list []string, v string) bool {
	return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(x, v) })
}

// Decide picks direct play, remux or transcode for a file and audio track.
func Decide(f library.MediaFile, audioIndex int, d Device) Plan {
	return DecideWithSubtitle(f, audioIndex, -1, false, d)
}

// DecideWithSubtitle is Decide for a viewer who also chose subtitle track sub
// (position in f.Subtitles, -1 = none). A bitmap track the device cannot draw,
// or any track with forceBurn, is burned in, which needs a video transcode.
func DecideWithSubtitle(f library.MediaFile, audioIndex, sub int, forceBurn bool, d Device) Plan {
	videoOK := f.VideoCodec == "" || has(d.VideoCodecs, f.VideoCodec)
	height := 0
	if f.Height != nil {
		height = *f.Height
	}
	tooBig := d.MaxHeight > 0 && height > d.MaxHeight
	tooFast := d.MaxBitrate > 0 && fileBitrate(f) > d.MaxBitrate
	hdrIssue := f.HDR != "" && !has(d.VideoCodecs, "hdr") // clients announce HDR support explicitly
	audioOK := true
	audioCodec := ""
	if audioIndex >= 0 && audioIndex < len(f.Audio) {
		audioCodec = f.Audio[audioIndex].Codec
		audioOK = has(d.AudioCodecs, audioCodec)
	}
	// Direct play needs the container and the first audio track (players pick track 0).
	direct := videoOK && audioOK && !tooBig && !tooFast && !hdrIssue && has(d.Containers, f.Container) && audioIndex <= 0
	burn := burnIn(f, sub, forceBurn, direct, d)
	if direct && burn == nil {
		return Plan{Method: "direct"}
	}
	p := Plan{AudioOnly: f.VideoCodec == "", Burn: burn}
	var reasons []string
	if !has(d.Containers, f.Container) {
		reasons = append(reasons, "container "+f.Container)
	}
	if !p.AudioOnly && (!videoOK || tooBig || hdrIssue || tooFast || burn != nil) {
		p.TranscodeVideo = true
		switch {
		case !videoOK:
			reasons = append(reasons, "video codec "+f.VideoCodec)
		case tooBig:
			reasons = append(reasons, fmt.Sprintf("resolution %dp", height))
			p.Height = d.MaxHeight
		case hdrIssue:
			reasons = append(reasons, f.HDR)
		}
		if tooFast {
			reasons = append(reasons, fmt.Sprintf("bitrate limit %d kbps", d.MaxBitrate/1000))
		}
		if burn != nil {
			reasons = append(reasons, "subtitle burn-in")
		}
		if d.MaxBitrate > 0 {
			// Leave room for the audio track.
			p.MaxBitrate = max(d.MaxBitrate-256_000, 300_000)
			if h := heightFor(p.MaxBitrate); height > h && (p.Height == 0 || p.Height > h) {
				p.Height = h
			}
		}
		p.ToneMap = f.HDR != ""
	}
	if !audioOK {
		p.TranscodeAudio = true
		reasons = append(reasons, "audio codec "+audioCodec)
	} else if audioIndex > 0 {
		reasons = append(reasons, "audio track")
	}
	p.Method = "remux"
	if p.TranscodeVideo {
		p.Method = "transcode"
	}
	p.Reason = strings.Join(reasons, ", ")
	return p
}
