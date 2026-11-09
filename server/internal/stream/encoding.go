package stream

import (
	"context"
	"errors"
	"fmt"
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
	MaxSessions    int    `json:"maxSessions"`           // 0 = unlimited
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

// ErrBusy means the configured number of simultaneous transcodes is reached.
var ErrBusy = errors.New("too many transcodes at once")

// nvencPreset maps the x264 preset names onto NVENC's p1 (fastest) .. p7 (best).
var nvencPreset = map[string]string{"ultrafast": "p1", "superfast": "p2", "veryfast": "p3", "faster": "p4", "fast": "p5", "medium": "p6", "slow": "p7"}

// videoArgs returns input options (before -i) and the encoder options for the
// configured hardware, given the software filter chain.
func (e Encoding) videoArgs(filters []string) (input, output []string) {
	q := strconv.Itoa(e.Quality)
	switch e.HWAccel {
	case "vaapi":
		input = []string{"-vaapi_device", e.VAAPIDevice}
		filters = append(filters, "format=nv12", "hwupload")
		output = []string{"-vf", join(filters), "-c:v", "h264_vaapi", "-qp", q}
	case "qsv":
		input = []string{"-init_hw_device", "qsv=hw", "-filter_hw_device", "hw"}
		filters = append(filters, "format=nv12", "hwupload=extra_hw_frames=64")
		output = []string{"-vf", join(filters), "-c:v", "h264_qsv", "-preset", e.Preset, "-global_quality", q}
	case "nvenc":
		filters = append(filters, "format=yuv420p")
		output = []string{"-vf", join(filters), "-c:v", "h264_nvenc", "-preset", nvencPreset[e.Preset], "-rc", "vbr", "-cq", q, "-b:v", "0"}
	default:
		filters = append(filters, "format=yuv420p")
		output = []string{"-vf", join(filters), "-c:v", "libx264", "-preset", e.Preset, "-crf", q, "-profile:v", "high"}
	}
	return input, output
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
