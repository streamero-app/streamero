package app

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/webui"
)

// NewRouter builds the root HTTP handler.
func NewRouter(state *State, h *api.Handler) (http.Handler, error) {
	apiServer, err := gen.NewServer(h, gen.WithPathPrefix("/api/v1"))
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	// Client IP resolution behind reverse proxies is handled later by a trusted-proxy
	// setting; chi's RealIP trusts spoofable headers and is deliberately not used.
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
	})
	r.Get("/readyz", func(w http.ResponseWriter, _ *http.Request) {
		if !state.Ready() {
			w.Header().Set("Retry-After", strconv.Itoa(api.RetryAfterSeconds))
			writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "starting", "error": state.LastError()})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"status": "ready"})
	})

	r.Mount("/api/v1", startingGuard(state, apiServer))
	r.Handle("/*", webui.Handler())
	return r, nil
}

// startingGuard answers 503 + Retry-After for every API call (except public
// system endpoints that handle it themselves) until the server is ready.
func startingGuard(state *State, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !state.Ready() && !strings.HasPrefix(r.URL.Path, "/api/v1/system/") {
			w.Header().Set("Retry-After", strconv.Itoa(api.RetryAfterSeconds))
			writeJSON(w, http.StatusServiceUnavailable, gen.Error{Code: "starting", Message: "server is starting"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
