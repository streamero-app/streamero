// Package stream serves media to players: direct play with byte ranges, HLS
// remux/transcode via ffmpeg and WebVTT subtitles, all behind signed URLs.
package stream

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Signer creates and checks short-lived URL signatures. Players such as AVPlayer,
// Chromecast or a plain <video> cannot send auth headers, so the URL carries proof.
type Signer struct {
	Key []byte
	Now func() time.Time
}

func (s Signer) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s Signer) mac(scope string, exp int64) string {
	m := hmac.New(sha256.New, s.Key)
	m.Write([]byte(scope))
	m.Write([]byte{0})
	m.Write([]byte(strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// Query returns "exp=…&sig=…" authorising every path that starts with scope for ttl.
func (s Signer) Query(scope string, ttl time.Duration) string {
	exp := s.now().Add(ttl).Unix()
	return url.Values{"exp": {strconv.FormatInt(exp, 10)}, "sig": {s.mac(scope, exp)}}.Encode()
}

// Valid reports whether the query signs scope and has not expired.
func (s Signer) Valid(scope string, q url.Values) bool {
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil || s.now().Unix() > exp {
		return false
	}
	return hmac.Equal([]byte(s.mac(scope, exp)), []byte(q.Get("sig")))
}

// ScopeOf returns the signing scope of a stream path: everything up to and
// including the second segment after /stream/ (e.g. "/stream/hls/<session>/").
func ScopeOf(path string) string {
	parts := strings.SplitN(strings.TrimPrefix(path, "/stream/"), "/", 3)
	if len(parts) < 2 {
		return ""
	}
	return "/stream/" + parts[0] + "/" + parts[1] + "/"
}
