package netx

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestRedactURL(t *testing.T) {
	for in, want := range map[string]string{
		"http://user:pa55@tv.lan:8080/playlist.m3u":                     "http://***@tv.lan:8080/playlist.m3u",
		"http://iptv.example/get.php?username=me&password=s3cret&type=m3u": "http://iptv.example/get.php?password=***&type=***&username=***",
		"http://iptv.example/live/me/s3cret/123.ts":                     "http://iptv.example/live/***/***/123.ts",
		"https://jf.example/Items?api_key=abc#frag":                     "https://jf.example/Items?api_key=***",
		"not a url":                                                     "not a url",
	} {
		if got := RedactURL(in); got != want {
			t.Errorf("RedactURL(%q) = %q, want %q", in, got, want)
		}
	}
	err := errors.New(`Get "http://iptv.example/live/me/s3cret/1.ts?token=t0k": dial tcp: refused`)
	got := RedactErr(err)
	for _, secret := range []string{"me/s3cret", "t0k"} {
		if strings.Contains(got, secret) {
			t.Errorf("secret %q left in %q", secret, got)
		}
	}
	if !strings.Contains(got, "iptv.example") || !strings.Contains(got, "dial tcp: refused") {
		t.Errorf("useful part lost: %q", got)
	}
	if RedactErr(nil) != "" {
		t.Error("nil error")
	}
}

func TestUntrustedForwardingWarning(t *testing.T) {
	untrustedForward.Store(false)
	h := Middleware(nil)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	if UntrustedForwarding() {
		t.Fatal("warning without a forwarded request")
	}
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	// With trusted proxies configured there is nothing to warn about.
	trusted, _ := ParsePrefixes("10.0.0.1")
	Middleware(trusted)(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {})).ServeHTTP(httptest.NewRecorder(), r)
	if UntrustedForwarding() {
		t.Fatal("warning although trusted proxies are set")
	}
	h.ServeHTTP(httptest.NewRecorder(), r)
	if !UntrustedForwarding() {
		t.Fatal("forwarded request without trusted proxies not noticed")
	}
}
