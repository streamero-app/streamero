package netx

import (
	"net/url"
	"regexp"
	"strings"
)

// Redaction of URLs in log lines and error messages (D-053). Tuner, playlist
// and import addresses often carry credentials: user:password@host, query
// parameters (?username=…&password=…, ?token=…) or, for IPTV panels, the path
// (/live/<user>/<password>/<id>.ts). Logs keep the scheme, host and the rest
// of the path, which is what is needed to diagnose a failure.

const mask = "***"

var urlInText = regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.\-]*://[^\s"'<>]+`)

// credentialPath matches the IPTV panel layout /<kind>/<user>/<password>/...
var credentialPath = regexp.MustCompile(`^/(live|movie|series|timeshift)/[^/]+/[^/]+/`)

// RedactURL returns raw with its user info, query values and panel-style path
// credentials replaced by ***. Text that is not a URL is returned unchanged.
func RedactURL(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme == "" {
		return raw
	}
	if u.User != nil {
		u.User = url.User(mask)
	}
	if u.RawQuery != "" {
		q := u.Query()
		for k := range q {
			q[k] = []string{mask}
		}
		u.RawQuery = strings.ReplaceAll(q.Encode(), url.QueryEscape(mask), mask)
	}
	if m := credentialPath.FindStringSubmatch(u.Path); m != nil {
		u.Path = "/" + m[1] + "/" + mask + "/" + mask + "/" + u.Path[len(m[0]):]
		u.RawPath = ""
	}
	u.Fragment, u.RawFragment = "", ""
	return strings.ReplaceAll(u.String(), url.PathEscape(mask), mask)
}

// Redact replaces every URL inside s (an error message, ffmpeg output) by its
// redacted form.
func Redact(s string) string {
	return urlInText.ReplaceAllStringFunc(s, RedactURL)
}

// RedactErr is Redact for an error ("" for nil), for log attributes.
func RedactErr(err error) string {
	if err == nil {
		return ""
	}
	return Redact(err.Error())
}
