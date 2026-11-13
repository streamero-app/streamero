package stream

import (
	"fmt"
	"strconv"
	"strings"
)

// ShiftVTT moves every cue of a WebVTT document offsetMs earlier. A transcoded
// HLS stream that starts at startMs plays from time 0 (ffmpeg -ss without
// -copyts), so its sideloaded subtitles must start at 0 as well: cues that end
// before the new zero are dropped, cues that straddle it start at 0.
func ShiftVTT(doc []byte, offsetMs int64) []byte {
	if offsetMs <= 0 {
		return doc
	}
	text := strings.ReplaceAll(string(doc), "\r\n", "\n")
	blocks := strings.Split(text, "\n\n")
	out := make([]string, 0, len(blocks))
	for _, b := range blocks {
		lines := strings.Split(b, "\n")
		timing := -1
		for i, l := range lines {
			if strings.Contains(l, "-->") {
				timing = i
				break
			}
		}
		// Header, NOTE, STYLE and REGION blocks (and anything unparseable) stay as they are.
		if timing < 0 || strings.HasPrefix(strings.TrimSpace(lines[0]), "NOTE") {
			out = append(out, b)
			continue
		}
		start, end, settings, ok := parseTiming(lines[timing])
		if !ok {
			out = append(out, b)
			continue
		}
		start, end = start-offsetMs, end-offsetMs
		if end <= 0 {
			continue
		}
		lines[timing] = vttTime(max(0, start)) + " --> " + vttTime(end) + settings
		out = append(out, strings.Join(lines, "\n"))
	}
	return []byte(strings.Join(out, "\n\n"))
}

// parseTiming reads "start --> end [settings]"; settings keep their leading space.
func parseTiming(line string) (start, end int64, settings string, ok bool) {
	a, rest, found := strings.Cut(line, "-->")
	if !found {
		return 0, 0, "", false
	}
	rest = strings.TrimLeft(rest, " \t")
	endS, settings, _ := strings.Cut(rest, " ")
	if settings != "" {
		settings = " " + settings
	}
	start, ok1 := parseVTTTime(strings.TrimSpace(a))
	end, ok2 := parseVTTTime(strings.TrimSpace(endS))
	return start, end, settings, ok1 && ok2
}

// parseVTTTime reads "hh:mm:ss.ttt" or "mm:ss.ttt" into milliseconds.
func parseVTTTime(s string) (int64, bool) {
	whole, frac, _ := strings.Cut(s, ".")
	parts := strings.Split(whole, ":")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, false
	}
	var ms int64
	for _, p := range parts {
		n, err := strconv.ParseInt(p, 10, 64)
		if err != nil || n < 0 {
			return 0, false
		}
		ms = ms*60 + n
	}
	ms *= 1000
	if frac != "" {
		if len(frac) > 3 {
			frac = frac[:3]
		}
		for len(frac) < 3 {
			frac += "0"
		}
		n, err := strconv.ParseInt(frac, 10, 64)
		if err != nil {
			return 0, false
		}
		ms += n
	}
	return ms, true
}

func vttTime(ms int64) string {
	return fmt.Sprintf("%02d:%02d:%02d.%03d", ms/3_600_000, ms/60_000%60, ms/1000%60, ms%1000)
}
