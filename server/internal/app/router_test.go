package app

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api"
)

func newTestRouter(t *testing.T, state *State) http.Handler {
	t.Helper()
	r, err := NewRouter(state, &api.Handler{State: state, ServerName: "Test", StartedAt: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func do(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func TestStartingReturns503WithRetryAfter(t *testing.T) {
	h := newTestRouter(t, &State{})
	for _, path := range []string{"/readyz", "/api/v1/system/info", "/api/v1/anything"} {
		rec := do(h, path)
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", path, rec.Code)
		}
		if rec.Header().Get("Retry-After") == "" {
			t.Errorf("%s: missing Retry-After", path)
		}
	}
	if rec := do(h, "/healthz"); rec.Code != http.StatusOK {
		t.Errorf("healthz while starting: %d", rec.Code)
	}
	if rec := do(h, "/api/v1/system/ping"); rec.Code != http.StatusOK {
		t.Errorf("ping while starting: %d", rec.Code)
	}
}

func TestReadySystemInfo(t *testing.T) {
	state := &State{}
	id := uuid.New()
	state.serverID.Store(&id)
	state.ready.Store(true)
	rec := do(newTestRouter(t, state), "/api/v1/system/info")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["product"] != "Streamero" || body["id"] != id.String() || body["apiVersion"] != "v1" {
		t.Errorf("unexpected body: %v", body)
	}
}

func TestSPAFallback(t *testing.T) {
	rec := do(newTestRouter(t, &State{}), "/library/123")
	if rec.Code != http.StatusOK || rec.Header().Get("Content-Type") != "text/html; charset=utf-8" {
		t.Errorf("SPA fallback: %d %q", rec.Code, rec.Header().Get("Content-Type"))
	}
}
