// Package notify delivers server events to webhook and push targets
// (generic JSON webhook, Discord, ntfy, Gotify).
package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/settings"
)

const settingsKey = "notifications.targets"

// Event kinds.
const (
	ItemAdded       = "item.added"
	PlaybackStarted = "playback.started"
	LoginSucceeded  = "login.succeeded"
	LoginFailed     = "login.failed"
	ScanFailed      = "scan.failed"
	Test            = "test"
)

// Target is one destination. Token is stored but never returned by the API.
type Target struct {
	ID      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	Kind    string    `json:"kind"` // generic | discord | ntfy | gotify
	URL     string    `json:"url"`
	Token   string    `json:"token,omitempty"`
	Events  []string  `json:"events"`
	Enabled bool      `json:"enabled"`
}

// Event is something that happened on the server.
type Event struct {
	Kind    string         `json:"event"`
	Title   string         `json:"title"`
	Message string         `json:"message"`
	Data    map[string]any `json:"data,omitempty"`
	At      time.Time      `json:"at"`
}

// ErrInvalid is returned for targets that cannot be saved.
var ErrInvalid = errors.New("invalid notification target")

// Load returns the configured targets.
func Load(ctx context.Context, st *settings.Store) ([]Target, error) {
	var out []Target
	if _, err := st.Get(ctx, settingsKey, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// Save validates and stores targets; an empty token keeps the stored one of the same id.
func Save(ctx context.Context, st *settings.Store, targets []Target) ([]Target, error) {
	old, err := Load(ctx, st)
	if err != nil {
		return nil, err
	}
	for i := range targets {
		t := &targets[i]
		if err := Validate(*t); err != nil {
			return nil, err
		}
		if t.Token == "" {
			for _, o := range old {
				if o.ID == t.ID {
					t.Token = o.Token
				}
			}
		}
	}
	return targets, st.Set(ctx, settingsKey, targets)
}

// Validate checks kind and URL.
func Validate(t Target) error {
	if !slices.Contains([]string{"generic", "discord", "ntfy", "gotify"}, t.Kind) {
		return fmt.Errorf("%w: kind %q", ErrInvalid, t.Kind)
	}
	u, err := url.Parse(t.URL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("%w: url must be http(s)", ErrInvalid)
	}
	if t.Kind == "gotify" && t.Token == "" && t.ID == uuid.Nil {
		return fmt.Errorf("%w: Gotify needs an app token", ErrInvalid)
	}
	return nil
}

// Dispatcher sends events to the targets subscribed to them, in the background.
type Dispatcher struct {
	// Settings returns the store (nil until the server is ready).
	Settings func() *settings.Store
	Client   *http.Client
	Log      *slog.Logger
	// Server is the server name used as the sender.
	Server func(ctx context.Context) string

	mu      sync.Mutex
	cached  []Target
	cacheAt time.Time
}

func (d *Dispatcher) targets(ctx context.Context) []Target {
	d.mu.Lock()
	defer d.mu.Unlock()
	if time.Since(d.cacheAt) < 30*time.Second {
		return d.cached
	}
	st := d.Settings()
	if st == nil {
		return nil
	}
	t, err := Load(ctx, st)
	if err != nil {
		return d.cached
	}
	d.cached, d.cacheAt = t, time.Now()
	return t
}

// Invalidate drops the cached targets (after the admin saved new ones).
func (d *Dispatcher) Invalidate() {
	d.mu.Lock()
	d.cacheAt = time.Time{}
	d.mu.Unlock()
}

// Emit delivers e to every enabled target subscribed to its kind. It never blocks the caller.
func (d *Dispatcher) Emit(ctx context.Context, e Event) {
	if d == nil || d.Settings == nil {
		return
	}
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	for _, t := range d.targets(ctx) {
		if !t.Enabled || !slices.Contains(t.Events, e.Kind) {
			continue
		}
		go func(t Target) {
			sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 15*time.Second)
			defer cancel()
			if err := d.Send(sctx, t, e); err != nil && d.Log != nil {
				d.Log.Warn("notification failed", "target", t.Name, "event", e.Kind, "err", err)
			}
		}(t)
	}
}

func (d *Dispatcher) client() *http.Client {
	if d.Client != nil {
		return d.Client
	}
	return &http.Client{Timeout: 15 * time.Second}
}

// Send delivers one event to one target synchronously (also used by "Send test").
func (d *Dispatcher) Send(ctx context.Context, t Target, e Event) error {
	server := "Streamero"
	if d.Server != nil {
		server = d.Server(ctx)
	}
	var (
		body    []byte
		ctype   = "application/json"
		target  = t.URL
		headers = map[string]string{}
	)
	switch t.Kind {
	case "discord":
		body, _ = json.Marshal(map[string]any{
			"username": server,
			"embeds":   []map[string]any{{"title": e.Title, "description": e.Message, "color": 0xFFAA45, "timestamp": e.At.Format(time.RFC3339)}},
		})
	case "ntfy":
		body, ctype = []byte(e.Message), "text/plain; charset=utf-8"
		headers["Title"] = e.Title
		headers["Tags"] = "clapper"
		if t.Token != "" {
			headers["Authorization"] = "Bearer " + t.Token
		}
	case "gotify":
		u, err := url.Parse(strings.TrimRight(t.URL, "/") + "/message")
		if err != nil {
			return err
		}
		q := u.Query()
		q.Set("token", t.Token)
		u.RawQuery = q.Encode()
		target = u.String()
		body, _ = json.Marshal(map[string]any{"title": e.Title, "message": e.Message, "priority": 5})
	default:
		payload := struct {
			Event
			Server string `json:"server"`
		}{e, server}
		body, _ = json.Marshal(payload)
		if t.Token != "" {
			headers["Authorization"] = "Bearer " + t.Token
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, target, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", ctype)
	req.Header.Set("User-Agent", "Streamero")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	// The URL is configured by an administrator on purpose.
	resp, err := d.client().Do(req) //nolint:gosec // G107/G704: admin-configured webhook target
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))
	if resp.StatusCode >= 300 {
		return fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return nil
}
