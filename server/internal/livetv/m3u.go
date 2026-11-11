package livetv

import (
	"errors"
	"io"
	"net/url"
	"strings"
)

var (
	errTooLarge = errors.New("the file is too large")
	// ErrEmptyPlaylist means the playlist has no playable channel.
	ErrEmptyPlaylist = errors.New("the playlist has no channels")
)

// maxPlaylistBytes bounds an M3U playlist (large IPTV lists are a few MB).
const maxPlaylistBytes = 64 << 20

// M3UChannel is one entry of an extended M3U playlist.
type M3UChannel struct {
	Name   string
	TvgID  string
	TvgName string
	Logo   string
	Group  string
	Number string
	URL    string
	// UserAgent comes from #EXTVLCOPT:http-user-agent or a http-user-agent attribute.
	UserAgent string
}

// allowedStreamScheme reports whether a channel URL may be handed to ffmpeg.
// Local files and ffmpeg's special protocols (concat:, subfile:, pipe:…) are refused.
func allowedStreamScheme(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Host == "" && u.Scheme != "udp" && u.Scheme != "rtp" {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "udp", "rtp", "rtsp", "rtsps", "rtmp", "rtmps":
		return true
	}
	return false
}

// ParseM3U reads an (extended) M3U playlist. Attributes of #EXTINF lines
// (tvg-id, tvg-name, tvg-logo, group-title, tvg-chno, channel-number) are read
// in any order, quoted or not; #EXTGRP sets the group of the next entry;
// entries whose URL is not a network stream are skipped. The text may be
// UTF-8 (with or without BOM), UTF-16 with BOM, or Windows-1252.
func ParseM3U(r io.Reader) ([]M3UChannel, int, error) {
	b, err := readLimited(r, maxPlaylistBytes)
	if err != nil {
		return nil, 0, err
	}
	text := decodeText(b)
	var out []M3UChannel
	skipped := 0
	var cur *M3UChannel
	var group, agent string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		switch {
		case line == "":
		case strings.HasPrefix(strings.ToUpper(line), "#EXTINF:"):
			c := parseExtinf(line[len("#EXTINF:"):])
			cur = &c
		case strings.HasPrefix(strings.ToUpper(line), "#EXTGRP:"):
			group = strings.TrimSpace(line[len("#EXTGRP:"):])
		case strings.HasPrefix(strings.ToUpper(line), "#EXTVLCOPT:"):
			k, v, ok := strings.Cut(line[len("#EXTVLCOPT:"):], "=")
			if ok && strings.EqualFold(strings.TrimSpace(k), "http-user-agent") {
				agent = strings.TrimSpace(v)
			}
		case strings.HasPrefix(line, "#"):
			// #EXTM3U header and other directives
		default:
			c := M3UChannel{}
			if cur != nil {
				c = *cur
			}
			c.URL = line
			if c.Group == "" {
				c.Group = group
			}
			if c.UserAgent == "" {
				c.UserAgent = agent
			}
			if c.Name == "" {
				c.Name = c.TvgName
			}
			if c.Name == "" {
				c.Name = line
			}
			if allowedStreamScheme(line) {
				out = append(out, c)
			} else {
				skipped++
			}
			cur, group, agent = nil, "", ""
		}
	}
	if len(out) == 0 {
		return nil, skipped, ErrEmptyPlaylist
	}
	return out, skipped, nil
}

// parseExtinf reads `-1 key="value" key2=value2,Display name`.
func parseExtinf(s string) M3UChannel {
	attrs := map[string]string{}
	i := 0
	// duration
	for i < len(s) && s[i] != ' ' && s[i] != ',' && s[i] != '\t' {
		i++
	}
	name := ""
	for i < len(s) {
		for i < len(s) && (s[i] == ' ' || s[i] == '\t') {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == ',' {
			name = s[i+1:]
			break
		}
		// key
		k0 := i
		for i < len(s) && s[i] != '=' && s[i] != ' ' && s[i] != ',' {
			i++
		}
		key := strings.ToLower(s[k0:i])
		if i >= len(s) || s[i] != '=' {
			continue // a bare word: ignore
		}
		i++ // '='
		var val string
		if i < len(s) && (s[i] == '"' || s[i] == '\'') {
			q := s[i]
			i++
			v0 := i
			for i < len(s) && s[i] != q {
				i++
			}
			val = s[v0:i]
			if i < len(s) {
				i++
			}
		} else {
			v0 := i
			for i < len(s) && s[i] != ' ' && s[i] != ',' {
				i++
			}
			val = s[v0:i]
		}
		attrs[key] = strings.TrimSpace(val)
	}
	c := M3UChannel{Name: strings.TrimSpace(name), TvgID: attrs["tvg-id"], TvgName: attrs["tvg-name"],
		Logo: attrs["tvg-logo"], Group: attrs["group-title"], UserAgent: attrs["http-user-agent"]}
	for _, k := range []string{"tvg-chno", "channel-number", "tvg-num"} {
		if v := attrs[k]; v != "" && c.Number == "" {
			c.Number = v
		}
	}
	if c.Logo == "" {
		c.Logo = attrs["logo"]
	}
	return c
}
