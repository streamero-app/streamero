package api

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/arr"
	"github.com/streamero-app/streamero/server/internal/auth"
)

// defaultArr serves handlers that were not given their own proxy.
var defaultArr = &arr.Proxy{}

func (h *Handler) arrProxy() *arr.Proxy {
	if h.Arr != nil {
		return h.Arr
	}
	return defaultArr
}

// ListArrServices implements GET /arr/services.
func (h *Handler) ListArrServices(ctx context.Context) (*gen.ArrServiceList, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	out := &gen.ArrServiceList{Services: []gen.ArrService{}}
	for _, s := range h.arrProxy().Statuses(ctx, h.requestSettings(ctx)) {
		out.Services = append(out.Services, gen.ArrService{ID: gen.ArrServiceID(s.ID), Configured: s.Configured,
			Reachable: s.Reachable, Version: optString(s.Version), Error: optString(s.Error), ApiBase: s.APIBase})
	}
	return out, nil
}

// ArrProxyPrefix is where the proxy is mounted (outside the generated router,
// like /stream: arbitrary sub-paths and streamed bodies; D-048).
const ArrProxyPrefix = "/api/v1/arr/"

// ArrProxy serves /api/v1/arr/{service}/proxy/* for administrators (not kids profiles).
func (h *Handler) ArrProxy(w http.ResponseWriter, r *http.Request) {
	if !h.State.Ready() || h.Auth == nil || h.Auth() == nil {
		w.Header().Set("Retry-After", strconv.Itoa(RetryAfterSeconds))
		arr.WriteError(w, http.StatusServiceUnavailable, "starting", "server is starting")
		return
	}
	token, _ := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	p, err := h.Auth().Authenticate(r.Context(), strings.TrimSpace(token))
	if err != nil {
		if !errors.Is(err, auth.ErrUnauthenticated) {
			arr.WriteError(w, http.StatusInternalServerError, "internal", "internal server error")
			return
		}
		arr.WriteError(w, http.StatusUnauthorized, "unauthenticated", "sign in first")
		return
	}
	if p.IsKid() || !p.User.IsAdmin {
		arr.WriteError(w, http.StatusForbidden, "forbidden", "administrator rights required")
		return
	}
	svc, ok := arr.Lookup(chi.URLParam(r, "service"))
	if !ok {
		arr.WriteError(w, http.StatusNotFound, "not_found", "unknown service")
		return
	}
	escaped, ok := strings.CutPrefix(r.URL.EscapedPath(), ArrProxyPrefix+svc.ID+"/proxy/")
	if !ok {
		arr.WriteError(w, http.StatusBadRequest, "invalid_path", "this path is not allowed")
		return
	}
	base, key := svc.Conn(h.requestSettings(r.Context()))
	h.arrProxy().Serve(w, r, p.User.ID, svc, base, key, escaped)
}
