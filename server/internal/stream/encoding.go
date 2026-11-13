package stream

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"strconv"
)

// KeyEncoding is the settings key of the transcoding settings.
const KeyEncoding = "encoding"

// Encoding controls how ffmpeg transcodes when a device cannot play a file as-is.
type Encoding struct {
	HWAccel        string `json:"hardwareAcceleration"` // none, vaapi, qsv, nvenc
	VAAPIDevice    string `json:"vaapiDevice,omitempty"`
	Preset         string `json:"preset"`
	Quality        int    `json:"quality"` // CRF / CQ / QP
	AudioKbps      int    `json:"audioBitrateKbps"`
	SegmentSeconds int    `json:"segmentSeconds"`
	MaxSessions    int    `json:"maxSessions"`           // 0 = automatic (see SessionLimit)
	ToneMapping    *bool  `json:"toneMapping,omitempty"` // nil = on
}

// DefaultEncoding is used until an administrator changes something.
func DefaultEncoding() Encoding {
	return Encoding{HWAccel: "none", VAAPIDevice: "/dev/dri/renderD128", Preset: "veryfast", Quality: 22,
		AudioKbps: 192, SegmentSeconds: 6}
}

// Normalize fills missing values with the defaults.
func (e Encoding) Normalize() Encoding {
	d := DefaultEncoding()
	if e.HWAccel == "" {
		e.HWAccel = d.HWAccel
	}
	if e.VAAPIDevice == "" {
		e.VAAPIDevice = d.VAAPIDevice
	}
	if e.Preset == "" {
		e.Preset = d.Preset
	}
	if e.Quality == 0 {
		e.Quality = d.Quality
	}
	if e.AudioKbps == 0 {
		e.AudioKbps = d.AudioKbps
	}
	if e.SegmentSeconds == 0 {
		e.SegmentSeconds = d.SegmentSeconds
	}
	return e
}

// ToneMaps reports whether HDR is converted to SDR when transcoding (default on).
func (e Encoding) ToneMaps() bool { return e.ToneMapping == nil || *e.ToneMapping }

// ErrBusy means the number of simultaneous ffmpeg sessions (for the server or
// for one account) is reached.
var ErrBusy = errors.New("too many transcodes at once")

// PerUserSessions is how many ffmpeg sessions one non-admin account may run at
// once (D-044). A device's new stream replaces its previous one, so this only
// counts different devices (or browser sessions) of the same account; live
// channels are shared between viewers and are not counted per account.
const PerUserSessions = 3

// SessionLimit is the server-wide cap on running ffmpeg sessions (HLS, remux
// included, and DLNA): the administrator's value, or max(4, CPU cores) when
// none is set, so a flood of requests cannot start ffmpeg without bound.
func (e Encoding) SessionLimit() int {
	if e.MaxSessions > 0 {
		return e.MaxSessions
	}
	return max(4, runtime.NumCPU())
}

// nvencPreset maps the x264 preset names onto NVENC's p1 (fastest) .. p7 (best).
var nvencPreset = map[string]string{"ultrafast": "p1", "superfast": "p2", "veryfast": "p3", "faster": "p4", "fast": "p5", "medium": "p6", "slow": "p7"}

// videoArgs returns input options (before -i), the complete filter chain (the
// given software filters, then the upload to the configured hardware) and the
// encoder options. Decoding and filters such as subtitle overlays run in
// software; frames go to the GPU only for the encoder (D-035).
func (e Encoding) videoArgs(filters []string) (input, chain, codec []string) {
	q := strconv.Itoa(e.Quality)
	chain = append([]string(nil), filters...)
	switch e.HWAccel {
	case "vaapi":
		input = []string{"-vaapi_device", e.VAAPIDevice}
		chain = append(chain, "format=nv12", "hwupload")
		codec = []string{"-c:v", "h264_vaapi", "-qp", q}
	case "qsv":
		input = []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw"}
		chain = append(chain, "format=nv12", "hwupload=extra_hw_frames=64")
		codec = []string{"-c:v", "h264_qsv", "-preset", e.Preset, "-global_quality", q}
	case "nvenc":
		chain = append(chain, "format=yuv420p")
		codec = []string{"-c:v", "h264_nvenc", "-preset", nvencPreset[e.Preset], "-rc", "vbr", "-cq", q, "-b:v", "0"}
	default:
		chain = append(chain, "format=yuv420p")
		codec = []string{"-c:v", "libx264", "-preset", e.Preset, "-crf", q, "-profile:v", "high"}
	}
	return input, chain, codec
}

func join(filters []string) string {
	out := ""
	for i, f := range filters {
		if i > 0 {
			out += ","
		}
		out += f
	}
	return out
}

// gop is a keyframe every segment, so segments start cleanly.
func (e Encoding) gop() string { return fmt.Sprint(e.SegmentSeconds * 24) }

// Loader reads the current encoding settings.
type Loader func(ctx context.Context) Encoding
