package app

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/livetv"
)

// A kids profile on an administrator's account must not reach administrator
// features through the compat API, Live TV, or a deleted profile.
func TestKidProfileOnAdminAccountIntegration(t *testing.T) {
	srv := newAuthTestServer(t)
	adminC := newClient(t, srv)
	setupAdmin(t, adminC)
	if _, err := testPool.Exec(context.Background(), `DELETE FROM settings WHERE key = 'livetv'`); err != nil {
		t.Fatal(err)
	}
	tv := &livetv.Service{Pool: testPool, Settings: testState.Settings, Log: slog.New(slog.NewTextHandler(io.Discard, nil))}
	testState.SetLiveTV(tv)
	testState.Compat().LiveTV = tv
	t.Cleanup(func() { testState.Compat().LiveTV = nil })

	var kid struct {
		ID string `json:"id"`
	}
	if s := adminC.do("POST", "/profiles", map[string]any{"name": "Mia", "color": "#3dd68c", "isKid": true, "maxRating": 7}, &kid); s != 201 {
		t.Fatalf("create kid: HTTP %d", s)
	}
	// The child's tablet: signed in to the administrator's account, in the kids profile.
	tablet := newClient(t, srv)
	var login authResult
	if s := tablet.do("POST", "/auth/login", map[string]any{"username": "Admin", "password": "admin-pass-1",
		"device": map[string]any{"name": "tablet"}}, &login); s != 200 {
		t.Fatalf("tablet login: HTTP %d", s)
	}
	tablet.token = login.Token
	if s := tablet.do("POST", "/profiles/"+kid.ID+"/select", nil, nil); s != 204 {
		t.Fatalf("select kid: HTTP %d", s)
	}

	// Compat API: no admin endpoints, and the client is not told it is an administrator.
	tc := &compatCaller{t: t, base: srv.URL + "/compat", token: login.Token}
	for _, c := range []struct{ method, path string }{
		{"POST", "/Auth/Keys?App=Sneaky"}, {"GET", "/Auth/Keys"}, {"POST", "/Library/Refresh"}, {"POST", "/Library/Media/Updated"},
	} {
		if s := tc.call(c.method, c.path, nil, nil); s != 403 {
			t.Errorf("compat %s %s from kids profile: HTTP %d, want 403", c.method, c.path, s)
		}
	}
	var keys []map[string]any
	if adminC.do("GET", "/apikeys", nil, &keys); len(keys) != 0 {
		t.Fatalf("an API key was created from the kids profile: %v", keys)
	}
	var me struct{ Policy map[string]any }
	if s := tc.call("GET", "/Users/Me", nil, &me); s != 200 || me.Policy["IsAdministrator"] != false || me.Policy["EnableLiveTvAccess"] != false {
		t.Fatalf("compat me from kids profile: %d %v", s, me.Policy)
	}
	var users []map[string]any
	if tc.call("GET", "/Users", nil, &users); len(users) != 1 {
		t.Fatalf("compat users from kids profile: %v", users)
	}
	// Defence in depth: the auth service itself refuses.
	p, err := testState.Auth().Authenticate(context.Background(), login.Token)
	if err != nil || !p.IsKid() {
		t.Fatalf("principal: %+v %v", p, err)
	}
	if _, _, err := testState.Auth().CreateAPIKey(context.Background(), p, "x"); !errors.Is(err, auth.ErrKidProfile) {
		t.Fatalf("CreateAPIKey from a kids profile: %v", err)
	}

	// Live TV: not from a kids profile (no age ratings on channels and recordings).
	for _, path := range []string{"/livetv/channels", "/livetv/recordings"} {
		var e apiError
		if s := tablet.do("GET", path, nil, &e); s != 403 {
			t.Errorf("%s from kids profile: HTTP %d %+v", path, s, e)
		}
		if s := adminC.do("GET", path, nil, nil); s != 200 {
			t.Errorf("%s as admin: HTTP %d", path, s)
		}
	}
	var info struct{ IsEnabled bool }
	if tc.call("GET", "/LiveTv/Info", nil, &info); info.IsEnabled {
		t.Error("compat Live TV enabled for a kids profile")
	}

	// Deleting the profile signs the tablet out instead of promoting it to the account.
	if s := adminC.do("DELETE", "/profiles/"+kid.ID, nil, nil); s != 204 {
		t.Fatalf("delete profile: HTTP %d", s)
	}
	if s := tablet.do("GET", "/auth/me", nil, nil); s != 401 {
		t.Fatalf("tablet after its profile was deleted: HTTP %d, want 401", s)
	}
	if s := adminC.do("GET", "/auth/me", nil, nil); s != 200 {
		t.Fatalf("the deleting session was signed out: HTTP %d", s)
	}
}
