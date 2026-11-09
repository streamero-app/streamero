package library

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// AudioStream is an audio track (mirrors the API schema).
type AudioStream struct {
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
	Channels int    `json:"channels,omitempty"`
	Title    string `json:"title,omitempty"`
}

// SubtitleStream is an embedded or external (sidecar file) subtitle track.
type SubtitleStream struct {
	Codec    string `json:"codec"`
	Language string `json:"language,omitempty"`
	Forced   bool   `json:"forced,omitempty"`
	External bool   `json:"external"`
	Path     string `json:"path,omitempty"` // external only
}

// ProbeResult is the technical description of a media file.
type ProbeResult struct {
	Container  string
	DurationMS int64
	Width      int
	Height     int
	VideoCodec string
	HDR        string
	Audio      []AudioStream
	Subtitles  []SubtitleStream
	// Tags are the container's tags with lower-case keys (artist, album, track, ...).
	Tags map[string]string
	// HasCover: the file carries embedded cover art.
	HasCover bool
}

// Prober inspects a file (ffprobe in production, a fake in tests).
type Prober func(ctx context.Context, path string) (ProbeResult, error)

type ffprobeOutput struct {
	Format struct {
		FormatName string            `json:"format_name"`
		Duration   string            `json:"duration"`
		Tags       map[string]string `json:"tags"`
	} `json:"format"`
	Streams []struct {
		CodecType     string            `json:"codec_type"`
		CodecName     string            `json:"codec_name"`
		Width         int               `json:"width"`
		Height        int               `json:"height"`
		Channels      int               `json:"channels"`
		ColorTransfer string            `json:"color_transfer"`
		Tags          map[string]string `json:"tags"`
		Disposition   map[string]int    `json:"disposition"`
		SideData      []struct {
			Type string `json:"side_data_type"`
		} `json:"side_data_list"`
	} `json:"streams"`
}

// FFprobe runs the ffprobe binary (the image ships jellyfin-ffmpeg; only invoked, never linked).
func FFprobe(bin string) Prober {
	return func(ctx context.Context, path string) (ProbeResult, error) {
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		// #nosec G204 -- fixed binary; the path was validated by ResolveSafePath and is passed as one argument.
		out, err := exec.CommandContext(ctx, bin, "-v", "error", "-print_format", "json",
			"-show_format", "-show_streams", "--", path).Output()
		if err != nil {
			return ProbeResult{}, fmt.Errorf("ffprobe: %w", err)
		}
		return parseFFprobe(out, path)
	}
}

func parseFFprobe(out []byte, path string) (ProbeResult, error) {
	var o ffprobeOutput
	if err := json.Unmarshal(out, &o); err != nil {
		return ProbeResult{}, fmt.Errorf("ffprobe output: %w", err)
	}
	r := ProbeResult{Container: containerName(o.Format.FormatName, path), Tags: map[string]string{}}
	for k, v := range o.Format.Tags {
		if v = strings.TrimSpace(v); v != "" {
			r.Tags[strings.ToLower(k)] = v
		}
	}
	if d, err := strconv.ParseFloat(o.Format.Duration, 64); err == nil {
		r.DurationMS = int64(d * 1000)
	}
	for _, s := range o.Streams {
		lang := s.Tags["language"]
		if lang == "und" {
			lang = ""
		}
		switch s.CodecType {
		case "video":
			if s.Disposition["attached_pic"] == 1 {
				r.HasCover = true
				continue
			}
			if r.VideoCodec != "" {
				continue // a second video stream
			}
			r.VideoCodec, r.Width, r.Height = s.CodecName, s.Width, s.Height
			switch s.ColorTransfer {
			case "smpte2084":
				r.HDR = "HDR10"
			case "arib-std-b67":
				r.HDR = "HLG"
			}
			for _, sd := range s.SideData {
				if strings.Contains(sd.Type, "DOVI") || strings.Contains(sd.Type, "Dolby Vision") {
					r.HDR = "Dolby Vision"
				}
			}
		case "audio":
			for k, v := range s.Tags {
				if k = strings.ToLower(k); r.Tags[k] == "" && strings.TrimSpace(v) != "" && k != "language" && k != "handler_name" {
					r.Tags[k] = strings.TrimSpace(v)
				}
			}
			r.Audio = append(r.Audio, AudioStream{Codec: s.CodecName, Language: lang, Channels: s.Channels, Title: s.Tags["title"]})
		case "subtitle":
			r.Subtitles = append(r.Subtitles, SubtitleStream{Codec: s.CodecName, Language: lang, Forced: s.Disposition["forced"] == 1})
		}
	}
	return r, nil
}

func containerName(format, path string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch {
	case strings.Contains(format, "matroska"):
		if ext == "webm" {
			return "webm"
		}
		return "mkv"
	case strings.Contains(format, "mp4"):
		if ext == "m4v" || ext == "mov" {
			return ext
		}
		return "mp4"
	case format == "":
		return ext
	}
	return strings.SplitN(format, ",", 2)[0]
}

// versionBase strips a version label: "Movie (2021) - 2160p" → "Movie (2021)".
func versionBase(stem string) string {
	if i := strings.LastIndex(stem, " - "); i > 0 {
		return stem[:i]
	}
	return ""
}

// ExternalSubtitle describes a sidecar file like "Movie (2021).hr.forced.srt" for video "Movie (2021).mkv".
func ExternalSubtitle(videoPath, subPath string) (SubtitleStream, bool) {
	vbase := strings.TrimSuffix(filepath.Base(videoPath), filepath.Ext(videoPath))
	sbase := filepath.Base(subPath)
	ext := filepath.Ext(sbase)
	stem := strings.TrimSuffix(sbase, ext)
	// "Movie (2021).en.srt" belongs to "Movie (2021).mkv" and to its versions like
	// "Movie (2021) - 2160p.mkv"; the name must end at a dot ("Movie 2" ≠ "Movie 20").
	base := ""
	for _, cand := range []string{vbase, versionBase(vbase)} {
		if cand != "" && strings.HasPrefix(stem, cand) && (len(stem) == len(cand) || stem[len(cand)] == '.') {
			base = cand
			break
		}
	}
	if base == "" {
		return SubtitleStream{}, false
	}
	s := SubtitleStream{Codec: strings.TrimPrefix(strings.ToLower(ext), "."), External: true, Path: subPath}
	for _, part := range strings.Split(strings.TrimPrefix(stem[len(base):], "."), ".") {
		switch p := strings.ToLower(part); {
		case p == "forced":
			s.Forced = true
		case p == "default" || p == "sdh" || p == "cc" || p == "":
		case len(p) == 2 || len(p) == 3:
			s.Language = p
		}
	}
	return s, true
}
