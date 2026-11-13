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

// QueryUntil is Query with a fixed expiry, so the URL (and the browser cache
// entry behind it) stays the same for every request until then.
func (s Signer) QueryUntil(scope string, exp time.Time) string {
	e := exp.Unix()
	return url.Values{"exp": {strconv.FormatInt(e, 10)}, "sig": {s.mac(scope, e)}}.Encode()
}

// MaxTTL is the longest validity the server ever signs (Live TV images: up to
// 48 h, for a day-stable URL; stream links 8 h, compat tags 12 h, downloads 24 h)
// plus slack for clock steps. A signature expiring later than this was not made
// by this server's rules (e.g. minted with a leaked key) and is refused, so a
// rotated or restored key bounds how long old links live.
const MaxTTL = 48*time.Hour + 10*time.Minute

// Valid reports whether the query signs scope and has not expired.
func (s Signer) Valid(scope string, q url.Values) bool {
	exp, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	now := s.now()
	if err != nil || now.Unix() > exp || exp > now.Add(MaxTTL).Unix() {
		return false
	}
	return hmac.Equal([]byte(s.mac(scope, exp)), []byte(q.Get("sig")))
}

// Token is a compact signature ("<exp>.<sig>") for clients that carry one opaque
// value instead of a query string (the compat API's media source tag).
func (s Signer) Token(scope string, ttl time.Duration) string {
	exp := s.now().Add(ttl).Unix()
	return strconv.FormatInt(exp, 10) + "." + s.mac(scope, exp)
}

// ValidToken reports whether tok is an unexpired Token for scope.
func (s Signer) ValidToken(scope, tok string) bool {
	expS, sig, ok := strings.Cut(tok, ".")
	if !ok {
		return false
	}
	return s.Valid(scope, url.Values{"exp": {expS}, "sig": {sig}})
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
