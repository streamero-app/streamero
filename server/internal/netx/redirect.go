package netx

import (
	"errors"
	"net/http"
	"net/url"
	"strings"
)

// ErrCrossHostRedirect is returned for a redirect to another scheme or host.
var ErrCrossHostRedirect = errors.New("redirect to another host refused")

// SameOrigin reports whether two URLs have the same scheme and host (with port).
// A stored secret is only reused for the origin it was given for.
func SameOrigin(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && ua.Host != "" &&
		strings.EqualFold(ua.Scheme, ub.Scheme) && strings.EqualFold(ua.Host, ub.Host)
}

// SameHostRedirects is an http.Client CheckRedirect for clients that send an
// API key or token in a header: Go keeps custom headers (X-Api-Key, …) on
// redirects, so a redirect to another scheme or host would hand the secret over.
func SameHostRedirects(req *http.Request, via []*http.Request) error {
	if len(via) >= 10 {
		return errors.New("too many redirects")
	}
	first := via[0].URL
	if !strings.EqualFold(req.URL.Scheme, first.Scheme) || !strings.EqualFold(req.URL.Host, first.Host) {
		return ErrCrossHostRedirect
	}
	return nil
}
