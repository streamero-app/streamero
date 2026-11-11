package dlna

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// DLNA.ORG_FLAGS primary bits (DLNA guidelines, 4th field of protocolInfo).
const (
	flagTimeSeek     = 1 << 30 // LSOP time-based seek
	flagByteSeek     = 1 << 29 // LSOP byte-based seek
	flagStreaming    = 1 << 24 // streaming transfer mode
	flagInteractive  = 1 << 23 // interactive transfer mode (images)
	flagBackground   = 1 << 22 // background transfer mode
	flagStall        = 1 << 21 // connection stalling allowed (pause)
	flagDLNA15       = 1 << 20 // DLNA 1.5 version flag
	streamFlags      = flagStreaming | flagBackground | flagStall | flagDLNA15
	imageFlags       = flagInteractive | flagBackground | flagStall | flagDLNA15
	transferStream   = "Streaming"
	transferImage    = "Interactive"
	transferHeader   = "transferMode.dlna.org"
	featuresHeader   = "contentFeatures.dlna.org"
	timeSeekHeader   = "TimeSeekRange.dlna.org"
	captionHeader    = "CaptionInfo.sec"
	realTimeHeader   = "realTimeInfo.dlna.org"
	thumbnailProfile = "JPEG_TN"
)

func flags(primary uint32) string {
	return fmt.Sprintf("%08x%024d", primary, 0)
}

// Features builds the 4th protocolInfo field / contentFeatures.dlna.org value.
// pn may be empty (no DLNA profile name is claimed). byteSeek marks files
// served with Range support (DLNA.ORG_OP=01); timeSeek streams produced on
// the fly that accept TimeSeekRange (OP=10, CI=1: converted content).
func Features(pn string, byteSeek, timeSeek bool) string {
	op := "00"
	switch {
	case byteSeek && timeSeek:
		op = "11"
	case timeSeek:
		op = "10"
	case byteSeek:
		op = "01"
	}
	ci := "0"
	if timeSeek {
		ci = "1"
	}
	var parts []string
	if pn != "" {
		parts = append(parts, "DLNA.ORG_PN="+pn)
	}
	parts = append(parts, "DLNA.ORG_OP="+op, "DLNA.ORG_CI="+ci, "DLNA.ORG_FLAGS="+flags(streamFlags))
	return strings.Join(parts, ";")
}

// imageFeatures is contentFeatures for artwork.
func imageFeatures(pn string) string {
	return "DLNA.ORG_PN=" + pn + ";DLNA.ORG_OP=00;DLNA.ORG_CI=0;DLNA.ORG_FLAGS=" + flags(imageFlags)
}

// ProtocolInfo returns "http-get:*:<mime>:<features>".
func ProtocolInfo(mime, features string) string {
	return "http-get:*:" + mime + ":" + features
}

// audioPN names the DLNA profile of plain audio formats that have one.
func audioPN(mime string) string {
	switch mime {
	case "audio/mpeg":
		return "MP3"
	}
	return ""
}

// Duration formats a DIDL-Lite duration ("H:MM:SS.mmm").
func Duration(ms int64) string {
	if ms < 0 {
		ms = 0
	}
	h := ms / 3_600_000
	m := ms / 60_000 % 60
	s := ms / 1000 % 60
	return fmt.Sprintf("%d:%02d:%02d.%03d", h, m, s, ms%1000)
}

// npt formats seconds for TimeSeekRange ("123.456").
func npt(ms int64) string {
	return strconv.FormatFloat(float64(ms)/1000, 'f', 3, 64)
}

// ParseNPT reads a normal play time: "123.4", "1:02:03.5" or "02:03".
func ParseNPT(s string) (time.Duration, bool) {
	s = strings.TrimSpace(s)
	if s == "" || s == "now" {
		return 0, false
	}
	parts := strings.Split(s, ":")
	if len(parts) > 3 {
		return 0, false
	}
	var total float64
	for i, p := range parts {
		v, err := strconv.ParseFloat(p, 64)
		// Minutes and seconds after a colon stay below 60; the first field is unbounded.
		if err != nil || v < 0 || (i > 0 && v >= 60) {
			return 0, false
		}
		total = total*60 + v
	}
	return time.Duration(total * float64(time.Second)), true
}

// TimeSeek is a parsed TimeSeekRange.dlna.org request ("npt=10.5-" or "npt=0:01:00-0:02:00").
type TimeSeek struct {
	Start time.Duration
	End   time.Duration // 0 = to the end
}

// ParseTimeSeek parses the request header value.
func ParseTimeSeek(v string) (TimeSeek, bool) {
	v = strings.TrimSpace(v)
	rest, ok := strings.CutPrefix(strings.ToLower(v), "npt=")
	if !ok {
		return TimeSeek{}, false
	}
	a, b, ok := strings.Cut(rest, "-")
	if !ok {
		return TimeSeek{}, false
	}
	start, ok := ParseNPT(a)
	if !ok {
		return TimeSeek{}, false
	}
	ts := TimeSeek{Start: start}
	if b = strings.TrimSpace(b); b != "" {
		end, ok := ParseNPT(b)
		if !ok || end < start {
			return TimeSeek{}, false
		}
		ts.End = end
	}
	return ts, true
}

// timeSeekReply is the response header for a time seek over a title of durMS.
func timeSeekReply(startMS, durMS int64) string {
	if durMS <= 0 {
		return "npt=" + npt(startMS) + "-"
	}
	return "npt=" + npt(startMS) + "-" + npt(durMS) + "/" + npt(durMS)
}
