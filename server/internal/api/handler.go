// Package api implements the generated ogen handler interface.
package api

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/version"
)

// SystemState is what the API needs to know about the running server.
type SystemState interface {
	Ready() bool
	ServerID() uuid.UUID
	SetupCompleted(ctx context.Context) bool
}

// Handler implements gen.Handler.
type Handler struct {
	State      SystemState
	ServerName string // default when no name was set by an admin
	StartedAt  time.Time
	// Auth and Settings return nil until the server is ready; the router
	// answers 503 for non-system endpoints until then.
	Auth     func() *auth.Service
	Settings func() *settings.Store
	Library  func() *library.Service
	// NewTMDb builds a TMDb client (tests use a fake server).
	NewTMDb func(key string) *metadata.TMDb
}

var _ gen.Handler = (*Handler)(nil)

// RetryAfterSeconds is returned to clients while the server is starting.
const RetryAfterSeconds = 5

// GetSystemInfo implements GET /system/info.
func (h *Handler) GetSystemInfo(ctx context.Context) (gen.GetSystemInfoRes, error) {
	if !h.State.Ready() {
		return &gen.StartingHeaders{
			RetryAfter: gen.NewOptInt(RetryAfterSeconds),
			Response:   gen.Error{Code: "starting", Message: "server is starting"},
		}, nil
	}
	return &gen.SystemInfo{
		ID:             h.State.ServerID(),
		Name:           h.serverName(ctx),
		Product:        "Streamero",
		Version:        version.Version,
		Commit:         gen.NewOptString(version.Commit),
		ApiVersion:     version.APIVersion,
		SetupCompleted: h.State.SetupCompleted(ctx),
		StartedAt:      h.StartedAt,
	}, nil
}

// Ping implements GET /system/ping.
func (h *Handler) Ping(context.Context) (*gen.Pong, error) {
	return &gen.Pong{Ok: true}, nil
}

// StatusError lets handlers return an error with a specific HTTP status.
type StatusError struct {
	Status  int
	Code    string
	Message string
}

func (e *StatusError) Error() string { return e.Message }

// NewError maps handler errors to the common error response.
func (h *Handler) NewError(ctx context.Context, err error) *gen.ErrorStatusCode {
	if se := mapError(err); se != nil {
		return &gen.ErrorStatusCode{StatusCode: se.Status, Response: gen.Error{Code: se.Code, Message: se.Message}}
	}
	slog.ErrorContext(ctx, "request failed", "err", err)
	return &gen.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   gen.Error{Code: "internal", Message: "internal server error"},
	}
}

// ErrorHandler renders errors the generated server raises outside of handlers
// (e.g. router-level failures) in the common error format.
func ErrorHandler(ctx context.Context, w http.ResponseWriter, _ *http.Request, err error) {
	status, body := http.StatusInternalServerError, gen.Error{Code: "internal", Message: "internal server error"}
	if se := mapError(err); se != nil {
		status, body = se.Status, gen.Error{Code: se.Code, Message: se.Message}
	} else {
		slog.ErrorContext(ctx, "request failed", "err", err)
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}
