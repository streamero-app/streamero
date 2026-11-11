package dlna

import (
	"net/http"
	"slices"
	"strings"

	"github.com/streamero-app/streamero/server/internal/stream"
)

// Profile describes what a family of TVs plays directly and how it wants
// media and subtitles offered. Capabilities come from the manufacturers'
// published "supported formats" pages for their current TV ranges; anything
// a TV cannot play is remuxed or transcoded to MPEG-TS (H.264 + AAC) or MP3.
type Profile struct {
	Name  string // stable id, also carried in stream URLs
	Label string // shown in logs
	// Device lists the containers (video and audio), codecs and limits played as-is.
	Device stream.Device
	// TSAudio are the audio codecs passed through unchanged when the video is
	// repackaged as MPEG-TS (everything else becomes AAC stereo).
	TSAudio []string
	// MIME overrides the default content type per container.
	MIME map[string]string
	// CaptionHeader: answer the Samsung "getCaptionInfo.sec" request header and
	// add sec:CaptionInfoEx to items (subtitle file next to the video).
	CaptionHeader bool
	// SubtitleRes: offer the subtitle as an extra <res> (text/srt).
	SubtitleRes bool
}

var (
	videoCommon = []string{"h264", "hevc", "mpeg2video", "mpeg4"}
	tsAudio     = []string{"aac", "ac3", "eac3", "mp3", "mp2"}
)

// Profiles, most specific first; Generic is the fallback.
var (
	LGWebOS = Profile{Name: "lg", Label: "LG webOS",
		Device: stream.Device{
			Containers:  []string{"mp4", "m4v", "mov", "mkv", "webm", "mpegts", "avi", "mp3", "flac", "wav", "ogg"},
			VideoCodecs: append(slices.Clone(videoCommon), "vp9", "hdr"),
			AudioCodecs: []string{"aac", "ac3", "eac3", "mp3", "mp2", "flac", "vorbis", "pcm_s16le", "pcm_s24le"},
			MaxHeight:   2160},
		TSAudio: tsAudio, CaptionHeader: true, SubtitleRes: true}
	SamsungTizen = Profile{Name: "samsung", Label: "Samsung Tizen",
		Device: stream.Device{
			Containers:  []string{"mp4", "m4v", "mov", "mkv", "webm", "mpegts", "avi", "mp3", "flac", "wav", "ogg"},
			VideoCodecs: append(slices.Clone(videoCommon), "vp9", "hdr"),
			AudioCodecs: []string{"aac", "ac3", "eac3", "mp3", "mp2", "flac", "vorbis", "pcm_s16le", "pcm_s24le"},
			MaxHeight:   2160},
		TSAudio: tsAudio, MIME: map[string]string{"mkv": "video/x-mkv", "avi": "video/x-msvideo"}, CaptionHeader: true}
	SonyBravia = Profile{Name: "sony", Label: "Sony Bravia",
		Device: stream.Device{
			Containers:  []string{"mp4", "m4v", "mkv", "mpegts", "mp3", "flac", "wav"},
			VideoCodecs: []string{"h264", "hevc", "mpeg2video"},
			AudioCodecs: []string{"aac", "ac3", "eac3", "mp3", "flac", "pcm_s16le"},
			MaxHeight:   2160},
		TSAudio: []string{"aac", "ac3", "mp3"}, SubtitleRes: true}
	Generic = Profile{Name: "generic", Label: "generic DLNA renderer",
		Device: stream.Device{
			Containers:  []string{"mp4", "m4v", "mpegts", "mp3", "flac"},
			VideoCodecs: []string{"h264"},
			AudioCodecs: []string{"aac", "mp3", "flac"},
			MaxHeight:   1080},
		TSAudio: []string{"aac", "ac3", "mp3"}}
	profiles = []Profile{LGWebOS, SamsungTizen, SonyBravia, Generic}
)

// identity collects the headers renderers identify themselves with.
func identity(h http.Header) string {
	return strings.ToLower(strings.Join([]string{h.Get("User-Agent"), h.Get("X-AV-Client-Info"),
		h.Get("X-AV-Physical-Unit-Info"), h.Get("FriendlyName.DLNADEV.DLNA.ORG")}, " "))
}

// Detect picks the profile for a renderer from its request headers.
func Detect(h http.Header) Profile {
	id := identity(h)
	switch {
	case containsAny(id, "samsung", "sec_hhp", "tizen"):
		return SamsungTizen
	case containsAny(id, "webos", "web0s", "lge", "lg electronics", "netcast"):
		return LGWebOS
	case containsAny(id, "bravia", "sony"):
		return SonyBravia
	}
	return Generic
}

// ProfileByName returns a profile by its Name (Generic when unknown).
func ProfileByName(name string) Profile {
	for _, p := range profiles {
		if p.Name == name {
			return p
		}
	}
	return Generic
}

func containsAny(s string, subs ...string) bool {
	for _, x := range subs {
		if strings.Contains(s, x) {
			return true
		}
	}
	return false
}

var defaultMIME = map[string]string{
	"mp4": "video/mp4", "m4v": "video/mp4", "mov": "video/quicktime", "mkv": "video/x-matroska",
	"webm": "video/webm", "mpegts": "video/mpeg", "avi": "video/avi", "mp3": "audio/mpeg",
	"flac": "audio/flac", "wav": "audio/wav", "ogg": "audio/ogg",
}

// MIMEType returns the content type to announce for a container; audioOnly
// files in MP4/Ogg are announced as audio.
func (p Profile) MIMEType(container string, audioOnly bool) string {
	if m, ok := p.MIME[container]; ok {
		return m
	}
	if audioOnly {
		switch container {
		case "mp4", "m4v", "mov":
			return "audio/mp4"
		case "mkv", "webm":
			return "audio/x-matroska"
		}
	}
	if m, ok := defaultMIME[container]; ok {
		return m
	}
	if audioOnly {
		return "audio/" + container
	}
	return "video/" + container
}
