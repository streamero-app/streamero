package app

import (
	"net/http/httptest"
	"testing"
)

func TestCrossSiteWrite(t *testing.T) {
	for _, c := range []struct {
		name, method string
		hdr          map[string]string
		want         bool
	}{
		{"same-origin fetch", "POST", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://kino.example"}, false},
		{"proxy rewrote Host", "POST", map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://public.example"}, false},
		{"other site", "POST", map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}, true},
		{"sibling subdomain", "DELETE", map[string]string{"Sec-Fetch-Site": "same-site", "Origin": "https://evil.kino.example"}, true},
		{"old browser, other origin", "POST", map[string]string{"Origin": "https://evil.example"}, true},
		{"old browser, same origin", "POST", map[string]string{"Origin": "http://kino.example"}, false},
		{"forwarded host", "POST", map[string]string{"Origin": "https://public.example", "X-Forwarded-Host": "public.example"}, false},
		{"app without Origin", "POST", nil, false},
		{"read", "GET", map[string]string{"Sec-Fetch-Site": "cross-site"}, false},
		{"socket from another site", "GET", map[string]string{"Sec-Fetch-Site": "cross-site", "Upgrade": "websocket"}, true},
	} {
		r := httptest.NewRequest(c.method, "http://kino.example/api/v1/libraries", nil)
		for k, v := range c.hdr {
			r.Header.Set(k, v)
		}
		if got := crossSiteWrite(r); got != c.want {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
}
