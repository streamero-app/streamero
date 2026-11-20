package auth

import (
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
)

func TestReadOnlyRequest(t *testing.T) {
	for _, c := range []struct {
		method, path string
		upgrade      bool
		want         bool
	}{
		{"GET", "/api/v1/libraries", false, true},
		{"HEAD", "/api/stats", false, true},
		{"POST", "/api/v1/libraries", false, false},
		{"GET", "/api/v1/syncplay/socket", true, false},
		// GETs that start a transcode or a sign-in request.
		{"GET", "/Items/abc/PlaybackInfo", false, false},
		{"GET", "/compat/videos/abc/master.m3u8", false, false},
		{"GET", "/Videos/abc/main.m3u8", false, false},
		{"GET", "/QuickConnect/Initiate", false, false},
		{"GET", "/Videos/abc/stream.mkv", false, true},
	} {
		r := httptest.NewRequest(c.method, c.path, nil)
		if c.upgrade {
			r.Header.Set("Upgrade", "websocket")
		}
		if got := readOnlyRequest(r); got != c.want {
			t.Errorf("%s %s: read-only %v, want %v", c.method, c.path, got, c.want)
		}
	}
}

func TestPrincipalGrant(t *testing.T) {
	sid, pid, kid := uuid.New(), uuid.New(), uuid.New()
	if g := (Principal{SessionID: sid}).Grant(); g != "s_"+sid.String()+"_main" {
		t.Errorf("session grant %q", g)
	}
	if g := (Principal{SessionID: sid, Profile: &Profile{ID: pid}}).Grant(); g != "s_"+sid.String()+"_"+pid.String() {
		t.Errorf("profile grant %q", g)
	}
	if g := (Principal{APIKeyID: kid}).Grant(); g != "k_"+kid.String() {
		t.Errorf("key grant %q", g)
	}
	if g := (Principal{}).Grant(); g != "" {
		t.Errorf("empty grant %q", g)
	}
}
