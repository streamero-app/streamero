package api

import (
	"net/http"
	"strings"

	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/streamero-app/streamero/server/internal/library"
	"net/url"
	"runtime"
	"syscall"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/notify"
	"github.com/streamero-app/streamero/server/internal/version"
)

// Paths are the server's storage locations shown on the dashboard.
type Paths struct {
	Media     []string
	Cache     string
	Transcode string
}

func diskUsage(kind gen.StorageUsageKind, path string) (gen.StorageUsage, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(path, &st); err != nil {
		return gen.StorageUsage{}, false
	}
	bs := int64(st.Bsize)                                                                                                      //nolint:unconvert // Bsize is int32 on some platforms
	return gen.StorageUsage{Kind: kind, Path: path, TotalBytes: int64(st.Blocks) * bs, FreeBytes: int64(st.Bavail) * bs}, true //nolint:gosec // block counts fit int64
}

// GetDashboard implements GET /dashboard.
func (h *Handler) GetDashboard(ctx context.Context) (*gen.Dashboard, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	out := &gen.Dashboard{
		Server: gen.DashboardServer{Name: h.serverName(ctx), Version: version.Version, Commit: gen.NewOptString(version.Commit),
			Os: runtime.GOOS + "/" + runtime.GOARCH, StartedAt: h.StartedAt},
		Storage: []gen.StorageUsage{}, NowPlaying: []gen.NowPlaying{}, Activity: []gen.ActivityEntry{},
	}
	for _, p := range h.Paths.Media {
		if u, ok := diskUsage(gen.StorageUsageKindMedia, p); ok {
			out.Storage = append(out.Storage, u)
		}
	}
	for kind, p := range map[gen.StorageUsageKind]string{gen.StorageUsageKindCache: h.Paths.Cache, gen.StorageUsageKindTranscode: h.Paths.Transcode} {
		if u, ok := diskUsage(kind, p); ok && p != "" {
			out.Storage = append(out.Storage, u)
		}
	}
	if h.NowPlaying != nil {
		for _, s := range h.NowPlaying.Active() {
			np := gen.NowPlaying{Key: s.Key, User: s.User, Device: s.Device, ItemId: s.ItemID, Title: s.Title,
				Method: gen.NowPlayingMethod(s.Method), PositionMs: s.PositionMS, UpdatedAt: s.UpdatedAt}
			if s.Profile != "" {
				np.Profile = gen.NewOptString(s.Profile)
			}
			if s.Client != "" {
				np.Client = gen.NewOptString(s.Client)
			}
			if s.Reason != "" {
				np.Reason = gen.NewOptString(s.Reason)
			}
			if s.DurationMS > 0 {
				np.DurationMs = gen.NewOptInt64(s.DurationMS)
			}
			out.NowPlaying = append(out.NowPlaying, np)
		}
	}
	if h.Stream != nil && h.Stream() != nil && h.Stream().HLS != nil {
		out.ActiveTranscodes = h.Stream().HLS.Active()
	}
	act, err := h.activity(ctx, 40, 0)
	if err != nil {
		return nil, err
	}
	out.Activity = act
	return out, nil
}

// taskJobs maps task keys to the River job kinds that perform them.
var taskJobs = map[gen.TaskKey]string{
	gen.TaskKeyScanAll:         "library.scan_all",
	gen.TaskKeyRefreshMetadata: "metadata.refresh_all",
	gen.TaskKeyCleanupImages:   "images.cleanup",
	gen.TaskKeyAnalyzeMedia:    "media.analyze_all",
	gen.TaskKeyDetectSegments:  "media.segments_all",
	gen.TaskKeyBackup:          "backup.create",
	gen.TaskKeyRefreshGuide:    "livetv.guide_refresh_due",
}

var taskIntervals = map[gen.TaskKey]string{gen.TaskKeyScanAll: "6h", gen.TaskKeyCleanupImages: "24h", gen.TaskKeyAnalyzeMedia: "24h",
	gen.TaskKeyDetectSegments: "24h", gen.TaskKeyBackup: "24h", gen.TaskKeyRefreshGuide: "1h"}

// ListTasks implements GET /tasks.
func (h *Handler) ListTasks(ctx context.Context) ([]gen.Task, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	type last struct {
		state string
		at    *time.Time
	}
	seen := map[string]last{}
	running := map[string]bool{}
	rows, err := h.lib().Pool.Query(ctx, `SELECT kind, state::text, COALESCE(finalized_at, attempted_at)
		FROM river_job WHERE kind = ANY($1) ORDER BY id DESC LIMIT 500`,
		[]string{"library.scan_all", "metadata.refresh_all", "images.cleanup", "library.scan", "metadata.refresh_library", "media.analyze_all", "media.analyze",
			"media.segments_all", "media.segments", "backup.create", "livetv.guide_refresh_due", "livetv.guide_refresh"})
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var kind, state string
		var at *time.Time
		if err := rows.Scan(&kind, &state, &at); err != nil {
			return nil, err
		}
		if state == "running" || state == "available" {
			running[kind] = true
		}
		if _, ok := seen[kind]; !ok && (state == "completed" || state == "discarded" || state == "cancelled") {
			seen[kind] = last{state, at}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := []gen.Task{}
	for _, key := range []gen.TaskKey{gen.TaskKeyScanAll, gen.TaskKeyRefreshMetadata, gen.TaskKeyAnalyzeMedia, gen.TaskKeyDetectSegments, gen.TaskKeyCleanupImages, gen.TaskKeyBackup, gen.TaskKeyRefreshGuide} {
		kind := taskJobs[key]
		t := gen.Task{Key: key, Running: running[kind]}
		// The fan-out jobs finish at once; their per-library children are the real work.
		switch key {
		case gen.TaskKeyScanAll:
			t.Running = t.Running || running["library.scan"]
		case gen.TaskKeyRefreshMetadata:
			t.Running = t.Running || running["metadata.refresh_library"]
		case gen.TaskKeyAnalyzeMedia:
			t.Running = t.Running || running["media.analyze"]
		case gen.TaskKeyDetectSegments:
			t.Running = t.Running || running["media.segments"]
		case gen.TaskKeyRefreshGuide:
			t.Running = t.Running || running["livetv.guide_refresh"]
		}
		if l, ok := seen[kind]; ok && l.at != nil {
			t.LastRunAt = gen.NewOptDateTime(*l.at)
			state := l.state
			if state == "discarded" {
				state = "failed"
			}
			t.LastState = gen.NewOptString(state)
		}
		if iv, ok := taskIntervals[key]; ok {
			t.Interval = gen.NewOptString(iv)
		}
		out = append(out, t)
	}
	return out, nil
}

// RunTask implements POST /tasks/{taskKey}/run.
func (h *Handler) RunTask(ctx context.Context, params gen.RunTaskParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	if h.RunTaskFn == nil {
		return errStarting
	}
	if err := h.RunTaskFn(ctx, string(params.TaskKey)); err != nil {
		return err
	}
	if h.Auth() != nil {
		_ = h.Auth().Audit(ctx, &p.User.ID, "task.started", map[string]any{"task": string(params.TaskKey)})
	}
	return nil
}

func toTargets(ts []notify.Target) []gen.NotificationTarget {
	out := make([]gen.NotificationTarget, 0, len(ts))
	for _, t := range ts {
		u, _ := url.Parse(t.URL)
		events := make([]gen.NotificationEvent, 0, len(t.Events))
		for _, e := range t.Events {
			events = append(events, gen.NotificationEvent(e))
		}
		g := gen.NotificationTarget{ID: t.ID, Name: t.Name, Kind: gen.NotificationTargetKind(t.Kind), Events: events,
			Enabled: t.Enabled, HasToken: gen.NewOptBool(t.Token != "")}
		if u != nil {
			g.URL = *u
		}
		out = append(out, g)
	}
	return out
}

func fromTarget(g gen.NotificationTarget) notify.Target {
	t := notify.Target{ID: g.ID, Name: g.Name, Kind: string(g.Kind), URL: g.URL.String(), Token: g.Token.Value, Enabled: g.Enabled}
	for _, e := range g.Events {
		t.Events = append(t.Events, string(e))
	}
	return t
}

// GetNotificationSettings implements GET /settings/notifications.
func (h *Handler) GetNotificationSettings(ctx context.Context) (*gen.NotificationSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	ts, err := notify.Load(ctx, h.Settings())
	if err != nil {
		return nil, err
	}
	return &gen.NotificationSettings{Targets: toTargets(ts)}, nil
}

// UpdateNotificationSettings implements PUT /settings/notifications.
func (h *Handler) UpdateNotificationSettings(ctx context.Context, req *gen.NotificationSettings) (*gen.NotificationSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	in := make([]notify.Target, 0, len(req.Targets))
	for _, g := range req.Targets {
		t := fromTarget(g)
		if t.ID == uuid.Nil {
			t.ID = uuid.New()
		}
		in = append(in, t)
	}
	saved, err := notify.Save(ctx, h.Settings(), in)
	if errors.Is(err, notify.ErrInvalid) {
		return nil, &StatusError{Status: 400, Code: "invalid_notification_target", Message: err.Error()}
	}
	if err != nil {
		return nil, err
	}
	if h.Notify != nil {
		h.Notify.Invalidate()
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "notifications.updated", map[string]any{"targets": len(saved)})
	return &gen.NotificationSettings{Targets: toTargets(saved)}, nil
}

// TestNotification implements POST /settings/notifications/test.
func (h *Handler) TestNotification(ctx context.Context, req *gen.NotificationTarget) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	t := fromTarget(*req)
	if t.Token == "" && t.ID != uuid.Nil {
		if stored, err := notify.Load(ctx, h.Settings()); err == nil {
			t.Token = notify.StoredToken(t, stored)
		}
	}
	if err := notify.Validate(t); err != nil {
		return &StatusError{Status: 400, Code: "invalid_notification_target", Message: err.Error()}
	}
	d := h.Notify
	if d == nil {
		d = &notify.Dispatcher{}
	}
	err := d.Send(ctx, t, notify.Event{Kind: notify.Test, Title: "Streamero",
		Message: fmt.Sprintf("Test notification from %s.", h.serverName(ctx)), At: time.Now().UTC()})
	if err != nil {
		return &StatusError{Status: 502, Code: "notification_failed", Message: err.Error()}
	}
	return nil
}

// TrackPlayback records a playback start reported outside this API (compat clients).
func (h *Handler) TrackPlayback(ctx context.Context, p auth.Principal, itemID uuid.UUID, method, reason string, durationMS *int64, startMS int64) {
	h.trackPlayback(ctx, p, itemID, method, reason, durationMS, startMS)
}

// TrackProgress records a playback position reported outside this API (compat clients).
func (h *Handler) TrackProgress(p auth.Principal, itemID uuid.UUID, positionMS int64) {
	h.trackProgress(p, itemID, positionMS)
}

// trackPlayback records a playback start for the dashboard and notifications.
func (h *Handler) trackPlayback(ctx context.Context, p auth.Principal, itemID uuid.UUID, method, reason string, durationMS *int64, startMS int64) {
	if h.NowPlaying == nil || h.lib() == nil {
		return
	}
	key := p.SessionID.String()
	device, client := "API", ""
	if p.SessionID == uuid.Nil {
		key = "key:" + p.APIKeyID.String()
	} else {
		_ = h.lib().Pool.QueryRow(ctx, `SELECT device_name, client FROM sessions WHERE id = $1`, p.SessionID).Scan(&device, &client)
	}
	title := ""
	_ = h.lib().Pool.QueryRow(ctx, `SELECT COALESCE(metadata_title, title) FROM items WHERE id = $1`, itemID).Scan(&title)
	s := activity.Session{Key: key, User: p.User.Username, Device: device, Client: client, ItemID: itemID, Title: title,
		Method: method, Reason: reason, PositionMS: startMS}
	if p.Profile != nil {
		s.Profile = p.Profile.Name
	}
	if durationMS != nil {
		s.DurationMS = *durationMS
	}
	if h.NowPlaying.Start(s) && h.Notify != nil {
		who := p.User.Username
		if s.Profile != "" {
			who += " (" + s.Profile + ")"
		}
		h.Notify.Emit(ctx, notify.Event{Kind: notify.PlaybackStarted, Title: "▶ " + title,
			Message: fmt.Sprintf("%s · %s · %s", who, device, method),
			Data:    map[string]any{"itemId": itemID, "user": p.User.Username, "device": device, "method": method}})
	}
}

func (h *Handler) trackProgress(p auth.Principal, itemID uuid.UUID, positionMS int64) {
	if h.NowPlaying == nil {
		return
	}
	key := p.SessionID.String()
	if p.SessionID == uuid.Nil {
		key = "key:" + p.APIKeyID.String()
	}
	h.NowPlaying.Progress(key, itemID, positionMS)
}

// activity reads the audit log, newest first.
func (h *Handler) activity(ctx context.Context, limit, offset int) ([]gen.ActivityEntry, error) {
	rows, err := h.lib().Pool.Query(ctx, `SELECT a.at, COALESCE(u.username, ''), a.action, a.detail
		FROM audit_log a LEFT JOIN users u ON u.id = a.user_id ORDER BY a.at DESC, a.id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []gen.ActivityEntry{}
	for rows.Next() {
		var e gen.ActivityEntry
		var user string
		var raw []byte
		if err := rows.Scan(&e.At, &user, &e.Action, &raw); err != nil {
			return nil, err
		}
		if user != "" {
			e.User = gen.NewOptString(user)
		}
		var detail map[string]any
		if json.Unmarshal(raw, &detail) == nil && len(detail) > 0 {
			d := gen.ActivityEntryDetail{}
			for k, v := range detail {
				b, _ := json.Marshal(v)
				d[k] = b
			}
			e.Detail = gen.NewOptActivityEntryDetail(d)
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// GetActivity implements GET /admin/activity.
func (h *Handler) GetActivity(ctx context.Context, params gen.GetActivityParams) (*gen.ActivityPage, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	entries, err := h.activity(ctx, params.Limit.Or(50), params.Offset.Or(0))
	if err != nil {
		return nil, err
	}
	var total int
	if err := h.lib().Pool.QueryRow(ctx, `SELECT count(*) FROM audit_log`).Scan(&total); err != nil {
		return nil, err
	}
	return &gen.ActivityPage{Entries: entries, Total: total}, nil
}

// GetServerLogs implements GET /admin/logs.
func (h *Handler) GetServerLogs(ctx context.Context, params gen.GetServerLogsParams) ([]gen.LogEntry, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	out := []gen.LogEntry{}
	if h.Logs == nil {
		return out, nil
	}
	for _, e := range h.Logs.Recent(ctx, string(params.Level.Or(gen.GetServerLogsLevelInfo)), params.Q.Value, params.Limit.Or(300)) {
		le := gen.LogEntry{Time: e.Time, Level: gen.LogEntryLevel(e.Level), Message: e.Message, Source: e.Source}
		if e.Attrs != "" {
			le.Attrs = gen.NewOptString(e.Attrs)
		}
		out = append(out, le)
	}
	return out, nil
}

func sessionKey(p auth.Principal) string {
	if p.SessionID == uuid.Nil {
		return "key:" + p.APIKeyID.String()
	}
	return p.SessionID.String()
}

// SendSessionCommand implements POST /sessions/{sessionKey}/command.
func (h *Handler) SendSessionCommand(ctx context.Context, req *gen.SessionCommand, params gen.SendSessionCommandParams) error {
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	if h.NowPlaying == nil {
		return library.ErrNotFound
	}
	text := strings.TrimSpace(req.Text.Value)
	if req.Kind == gen.SessionCommandKindMessage && text == "" {
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "a message needs text"}
	}
	if !h.NowPlaying.Send(params.SessionKey, activity.Command{Kind: string(req.Kind), Text: text}) {
		return library.ErrNotFound
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "session."+string(req.Kind), map[string]any{"text": text})
	return nil
}

// TakeMyCommands implements GET /me/commands.
func (h *Handler) TakeMyCommands(ctx context.Context) ([]gen.SessionCommand, error) {
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out := []gen.SessionCommand{}
	if h.NowPlaying == nil {
		return out, nil
	}
	for _, c := range h.NowPlaying.Take(sessionKey(p)) {
		sc := gen.SessionCommand{Kind: gen.SessionCommandKind(c.Kind)}
		if c.Text != "" {
			sc.Text = gen.NewOptString(c.Text)
		}
		out = append(out, sc)
	}
	return out, nil
}
