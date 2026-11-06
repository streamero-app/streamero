package app

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/streamero-app/streamero/server/internal/api"
	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/netx"
	"github.com/streamero-app/streamero/server/internal/webui"
)

// RouterOptions configure client IP resolution and rate limiting.
type RouterOptions struct {
	TrustedProxies []netip.Prefix
	// AuthLimiter limits sign-in style endpoints per client IP (nil = no limit).
	AuthLimiter *netx.Limiter
}

// rateLimited lists the endpoints that accept secrets from anonymous or
// low-privilege callers (passwords, setup codes, invite tokens, PINs).
func rateLimited(r *http.Request) bool {
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && (p == "/api/v1/auth/login" || p == "/api/v1/setup" || p == "/api/v1/quickconnect/initiate"):
		return true
	case strings.HasPrefix(p, "/api/v1/invite/"):
		return true
	case r.Method == http.MethodPost && strings.HasPrefix(p, "/api/v1/profiles/") && strings.HasSuffix(p, "/select"):
		return true
	}
	return false
}

// NewRouter builds the root HTTP handler.
func NewRouter(state *State, h *api.Handler, opts ...RouterOptions) (http.Handler, error) {
	var o RouterOptions
	if len(opts) > 0 {
		o = opts[0]
	}
	apiServer, err := gen.NewServer(h, api.Security{Auth: state.Auth},
		gen.WithPathPrefix("/api/v1"), gen.WithErrorHandler(api.ErrorHandler))
	if err != nil {
		return nil, err
	}

	r := chi.NewRouter()
	// Client IP resolution behind reverse proxies is handled later by a trusted-proxy
	// setting; chi's RealIP trusts spoofable headers and is deliberately not used.
	r.Use(middleware.RequestID, middleware.Recoverer)
	r.Use(middleware.Timeout(60 * time.Second))
	r.Use(securityHeaders, netx.Middleware(o.TrustedProxies), sessionFromCookie)
	if o.AuthLimiter != nil {
		r.Use(netx.Limit(o.AuthLimiter, rateLimited))
	}

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

// sessionFromCookie lets the web client authenticate with the HttpOnly session
// cookie: it becomes a bearer token unless the request already carries one.
// It also records whether the client connection is HTTPS (for the Secure flag).
func sessionFromCookie(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "" {
			if c, err := r.Cookie(api.SessionCookie); err == nil && c.Value != "" {
				r.Header.Set("Authorization", "Bearer "+c.Value)
			}
		}
		// X-Forwarded-Proto only decides the cookie's Secure attribute, so trusting
		// it without a known-proxy list cannot weaken authentication.
		secure := r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
		next.ServeHTTP(w, r.WithContext(api.WithSecure(r.Context(), secure)))
	})
}

func securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "same-origin")
		h.Set("X-Frame-Options", "DENY")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
