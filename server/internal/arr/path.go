package arr

import (
	"errors"
	"net/url"
	"strings"
)

// ErrInvalidPath is a proxied path that could leave the service's API.
var ErrInvalidPath = errors.New("invalid path")

// CleanPath validates the escaped sub-path a client asked for (the part after
// /proxy/) and returns it, still escaped, for forwarding. It refuses anything
// that could reach outside the service's API: dot segments, empty segments
// ("//", which also covers absolute URLs), backslashes, control characters,
// and percent-encoded separators, dots, percent signs or control characters
// (so a decoded form can never differ in structure from what was checked).
func CleanPath(s Service, escaped string) (string, error) {
	if escaped == "" || strings.HasPrefix(escaped, "/") {
		return "", ErrInvalidPath
	}
	for i := 0; i < len(escaped); i++ {
		c := escaped[i]
		if c < 0x20 || c == 0x7f || c == '\\' {
			return "", ErrInvalidPath
		}
		if c != '%' {
			continue
		}
		if i+2 >= len(escaped) || !isHex(escaped[i+1]) || !isHex(escaped[i+2]) {
			return "", ErrInvalidPath
		}
		b := unhex(escaped[i+1])<<4 | unhex(escaped[i+2])
		if b < 0x20 || b == 0x7f || b == '/' || b == '\\' || b == '.' || b == '%' {
			return "", ErrInvalidPath
		}
		i += 2
	}
	decoded, err := url.PathUnescape(escaped)
	if err != nil {
		return "", ErrInvalidPath
	}
	segs := strings.Split(decoded, "/")
	for i, seg := range segs {
		// A trailing slash is fine; an empty segment anywhere else is "//".
		if (seg == "" && i != len(segs)-1) || seg == "." || seg == ".." {
			return "", ErrInvalidPath
		}
	}
	lower := strings.ToLower(decoded)
	if strings.HasPrefix(lower, "api/") {
		return escaped, nil
	}
	for _, e := range s.Extra {
		if strings.HasPrefix(lower, e) {
			return escaped, nil
		}
	}
	return "", ErrInvalidPath
}

func isHex(c byte) bool {
	return ('0' <= c && c <= '9') || ('a' <= c && c <= 'f') || ('A' <= c && c <= 'F')
}

func unhex(c byte) byte {
	switch {
	case '0' <= c && c <= '9':
		return c - '0'
	case 'a' <= c && c <= 'f':
		return c - 'a' + 10
	}
	return c - 'A' + 10
}

// FilterQuery drops API key parameters from a raw query string and keeps the
// rest exactly as the client sent it (order and encoding).
func FilterQuery(raw string) string {
	if raw == "" {
		return ""
	}
	parts := strings.Split(raw, "&")
	kept := parts[:0]
	for _, p := range parts {
		if p == "" {
			continue
		}
		name, _, _ := strings.Cut(p, "=")
		if n, err := url.QueryUnescape(name); err != nil || strings.EqualFold(n, "apikey") || strings.EqualFold(n, "api_key") {
			continue
		}
		kept = append(kept, p)
	}
	return strings.Join(kept, "&")
}

// Target joins the stored base URL (keeping any URL base such as
// http://radarr:7878/radarr), a path from CleanPath and a query from FilterQuery.
func Target(base, escapedPath, query string) (*url.URL, error) {
	b, err := url.Parse(base)
	if err != nil || b.Host == "" {
		return nil, errors.New("invalid base URL")
	}
	t := *b
	t.User, t.Fragment, t.RawFragment = nil, "", ""
	decoded, err := url.PathUnescape(escapedPath)
	if err != nil {
		return nil, ErrInvalidPath
	}
	t.Path = strings.TrimRight(b.Path, "/") + "/" + decoded
	t.RawPath = strings.TrimRight(b.EscapedPath(), "/") + "/" + escapedPath
	t.RawQuery = query
	return &t, nil
}
