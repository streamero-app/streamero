// Package api implements the generated ogen handler interface.
package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
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
	ServerName string
	StartedAt  time.Time
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
		Name:           h.ServerName,
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
func (h *Handler) NewError(_ context.Context, err error) *gen.ErrorStatusCode {
	var se *StatusError
	if errors.As(err, &se) {
		return &gen.ErrorStatusCode{StatusCode: se.Status, Response: gen.Error{Code: se.Code, Message: se.Message}}
	}
	return &gen.ErrorStatusCode{
		StatusCode: http.StatusInternalServerError,
		Response:   gen.Error{Code: "internal", Message: "internal server error"},
	}
}
