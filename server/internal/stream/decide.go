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
}

// Plan says how a file will be delivered.
type Plan struct {
	Method         string // direct | remux | transcode
	Reason         string
	TranscodeVideo bool
	TranscodeAudio bool
	Height         int  // target height when scaling (0 = keep)
	ToneMap        bool // HDR → SDR
}

func has(list []string, v string) bool {
	return slices.ContainsFunc(list, func(x string) bool { return strings.EqualFold(x, v) })
}

// Decide picks direct play, remux or transcode for a file and audio track.
// allowTranscode is the user's permission; without it only direct/remux are allowed.
func Decide(f library.MediaFile, audioIndex int, d Device) Plan {
	videoOK := f.VideoCodec == "" || has(d.VideoCodecs, f.VideoCodec)
	height := 0
	if f.Height != nil {
		height = *f.Height
	}
	tooBig := d.MaxHeight > 0 && height > d.MaxHeight
	hdrIssue := f.HDR != "" && !has(d.VideoCodecs, "hdr") // clients announce HDR support explicitly
	audioOK := true
	audioCodec := ""
	if audioIndex >= 0 && audioIndex < len(f.Audio) {
		audioCodec = f.Audio[audioIndex].Codec
		audioOK = has(d.AudioCodecs, audioCodec)
	}
	// Direct play needs the container and the first audio track (players pick track 0).
	if videoOK && audioOK && !tooBig && !hdrIssue && has(d.Containers, f.Container) && audioIndex <= 0 {
		return Plan{Method: "direct"}
	}
	p := Plan{}
	var reasons []string
	if !has(d.Containers, f.Container) {
		reasons = append(reasons, "container "+f.Container)
	}
	if !videoOK || tooBig || hdrIssue {
		p.TranscodeVideo = true
		switch {
		case !videoOK:
			reasons = append(reasons, "video codec "+f.VideoCodec)
		case tooBig:
			reasons = append(reasons, fmt.Sprintf("resolution %dp", height))
			p.Height = d.MaxHeight
		default:
			reasons = append(reasons, f.HDR)
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
