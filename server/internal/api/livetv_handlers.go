package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/livetv"
	"github.com/streamero-app/streamero/server/internal/stream"
)

var errNoLiveTV = &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "Live TV is not allowed for this account"}
var errNoLiveTVManage = &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "managing Live TV is not allowed for this account"}

func (h *Handler) tv() (*livetv.Service, error) {
	if h.LiveTV == nil || h.LiveTV() == nil || h.lib() == nil {
		return nil, errStarting
	}
	return h.LiveTV(), nil
}

// liveViewer: the liveTv permission (admins always).
func liveViewer(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if !p.User.IsAdmin && !p.User.Permissions.LiveTV {
		return p, errNoLiveTV
	}
	return p, nil
}

// liveManager: the manageLiveTv permission (admins always), never from a kids profile.
func liveManager(ctx context.Context) (auth.Principal, error) {
	p, err := adult(ctx)
	if err != nil {
		return p, err
	}
	if !p.User.IsAdmin && !p.User.Permissions.ManageLiveTV {
		return p, errNoLiveTVManage
	}
	return p, nil
}

func mapLiveTVError(err error) *StatusError {
	var inv *livetv.InvalidError
	switch {
	case errors.As(err, &inv):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: inv.Msg}
	case errors.Is(err, livetv.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: "not found"}
	case errors.Is(err, livetv.ErrTunersBusy):
		return &StatusError{Status: http.StatusServiceUnavailable, Code: "tuners_busy", Message: err.Error()}
	case errors.Is(err, livetv.ErrChannelUnavailable):
		return &StatusError{Status: http.StatusBadGateway, Code: "channel_unavailable", Message: "the channel could not be tuned"}
	case errors.Is(err, livetv.ErrRecordingDone):
		return &StatusError{Status: http.StatusConflict, Code: "recording_finished", Message: err.Error()}
	case errors.Is(err, livetv.ErrForbidden):
		return &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: err.Error()}
	}
	return nil
}

func optStr(v string) gen.OptString { return optString(v) }

func optIntP(v *int) gen.OptInt {
	if v == nil {
		return gen.OptInt{}
	}
	return gen.NewOptInt(*v)
}

func optUUIDP(v *uuid.UUID) gen.OptUUID {
	if v == nil {
		return gen.OptUUID{}
	}
	return gen.NewOptUUID(*v)
}

func optCat(c string) gen.OptLiveTvCategory {
	if c == "" {
		return gen.OptLiveTvCategory{}
	}
	return gen.NewOptLiveTvCategory(gen.LiveTvCategory(c))
}

func toLiveProgram(p livetv.Program) gen.LiveTvProgram {
	cats := p.Categories
	if cats == nil {
		cats = []string{}
	}
	out := gen.LiveTvProgram{ID: p.ID, ChannelId: p.ChannelID, ChannelName: optStr(p.ChannelName), ChannelNumber: optStr(p.ChannelNumber),
		Title: p.Title, Subtitle: optStr(p.SubTitle), Description: optStr(p.Description), Start: p.Start, End: p.End,
		Categories: cats, Category: optCat(p.Category), Season: optIntP(p.Season), Episode: optIntP(p.Episode), Year: optIntP(p.Year),
		IsNew: p.IsNew, ImageUrl: optStr(p.ImageURL), RecordingId: optUUIDP(p.RecordingID), SeriesRuleId: optUUIDP(p.SeriesRuleID)}
	if p.IsPremiere {
		out.IsPremiere = gen.NewOptBool(true)
	}
	if p.IsRepeat {
		out.IsRepeat = gen.NewOptBool(true)
	}
	if p.IsLive {
		out.IsLive = gen.NewOptBool(true)
	}
	if p.RecordingStatus != "" {
		out.RecordingStatus = gen.NewOptLiveTvRecordingStatus(gen.LiveTvRecordingStatus(p.RecordingStatus))
	}
	return out
}

func toLiveChannel(c livetv.Channel) gen.LiveTvChannel {
	out := gen.LiveTvChannel{ID: c.ID, Number: c.Number, Name: c.Name, Group: optStr(c.Group), Category: optCat(c.Category),
		LogoUrl: optStr(c.LogoURL), TunerId: c.TunerID, Favorite: c.Favorite}
	if c.HD {
		out.Hd = gen.NewOptBool(true)
	}
	if c.Now != nil {
		out.Now = gen.NewOptLiveTvProgram(toLiveProgram(*c.Now))
	}
	if c.Next != nil {
		out.Next = gen.NewOptLiveTvProgram(toLiveProgram(*c.Next))
	}
	return out
}

// ListLiveTvChannels implements GET /livetv/channels.
func (h *Handler) ListLiveTvChannels(ctx context.Context, params gen.ListLiveTvChannelsParams) ([]gen.LiveTvChannel, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveViewer(ctx)
	if err != nil {
		return nil, err
	}
	list, err := svc.Channels(ctx, stream.Owner(p), livetv.ChannelFilter{Category: string(params.Category.Value), FavoritesOnly: params.Favorites.Value})
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvChannel, 0, len(list))
	for _, c := range list {
		out = append(out, toLiveChannel(c))
	}
	return out, nil
}

// GetLiveTvChannel implements GET /livetv/channels/{channelId}.
func (h *Handler) GetLiveTvChannel(ctx context.Context, params gen.GetLiveTvChannelParams) (*gen.LiveTvChannel, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveViewer(ctx)
	if err != nil {
		return nil, err
	}
	c, err := svc.ChannelByID(ctx, stream.Owner(p), params.ChannelId)
	if err != nil {
		return nil, err
	}
	out := toLiveChannel(c)
	return &out, nil
}

// AddLiveTvFavorite implements PUT /livetv/channels/{channelId}/favorite.
func (h *Handler) AddLiveTvFavorite(ctx context.Context, params gen.AddLiveTvFavoriteParams) error {
	return h.setFavorite(ctx, params.ChannelId, true)
}

// RemoveLiveTvFavorite implements DELETE /livetv/channels/{channelId}/favorite.
func (h *Handler) RemoveLiveTvFavorite(ctx context.Context, params gen.RemoveLiveTvFavoriteParams) error {
	return h.setFavorite(ctx, params.ChannelId, false)
}

func (h *Handler) setFavorite(ctx context.Context, id uuid.UUID, on bool) error {
	svc, err := h.tv()
	if err != nil {
		return err
	}
	p, err := liveViewer(ctx)
	if err != nil {
		return err
	}
	return svc.SetFavorite(ctx, stream.Owner(p), id, on)
}

// PlayLiveTvChannel implements POST /livetv/channels/{channelId}/play.
func (h *Handler) PlayLiveTvChannel(ctx context.Context, req *gen.LiveTvPlayRequest, params gen.PlayLiveTvChannelParams) (*gen.LiveTvPlayback, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveViewer(ctx)
	if err != nil {
		return nil, err
	}
	d := req.Device
	dev := stream.Device{Containers: d.Containers, VideoCodecs: d.VideoCodecs, AudioCodecs: d.AudioCodecs, HLS: d.Hls.Value,
		MaxHeight: d.MaxHeight.Value, MaxBitrate: d.MaxBitrate.Value}
	if prefs, err := h.lib().GetPrefs(ctx, stream.Owner(p)); err == nil {
		if prefs.MaxBitrateKbps > 0 && (dev.MaxBitrate == 0 || dev.MaxBitrate > prefs.MaxBitrateKbps*1000) {
			dev.MaxBitrate = prefs.MaxBitrateKbps * 1000
		}
		if prefs.MaxHeight > 0 && (dev.MaxHeight == 0 || dev.MaxHeight > prefs.MaxHeight) {
			dev.MaxHeight = prefs.MaxHeight
		}
	}
	pb, err := svc.Play(ctx, p, params.ChannelId, dev)
	if err != nil {
		if errors.Is(err, stream.ErrTranscodeDenied) {
			return nil, &StatusError{Status: http.StatusForbidden, Code: "transcode_denied", Message: err.Error()}
		}
		if errors.Is(err, stream.ErrBusy) {
			return nil, &StatusError{Status: http.StatusServiceUnavailable, Code: "transcoder_busy", Message: err.Error()}
		}
		return nil, err
	}
	out := &gen.LiveTvPlayback{ChannelId: params.ChannelId, URL: pb.URL, Method: gen.LiveTvPlaybackMethod(pb.Method),
		Reason: optStr(pb.Reason), TimeshiftSeconds: pb.TimeshiftSeconds}
	if pb.Now != nil {
		out.Now = gen.NewOptLiveTvProgram(toLiveProgram(*pb.Now))
	}
	return out, nil
}

// GetLiveTvGuide implements GET /livetv/guide.
func (h *Handler) GetLiveTvGuide(ctx context.Context, params gen.GetLiveTvGuideParams) (*gen.LiveTvGuide, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveViewer(ctx)
	if err != nil {
		return nil, err
	}
	start := time.Now().UTC().Truncate(30 * time.Minute)
	if v, ok := params.Start.Get(); ok {
		start = v.UTC()
	}
	hours := 3
	if v, ok := params.Hours.Get(); ok {
		hours = v
	}
	end := start.Add(time.Duration(hours) * time.Hour)
	rows, err := svc.Guide(ctx, stream.Owner(p), start, end, livetv.ChannelFilter{Category: string(params.Category.Value), FavoritesOnly: params.Favorites.Value})
	if err != nil {
		return nil, err
	}
	out := &gen.LiveTvGuide{Start: start, End: end, Rows: make([]gen.LiveTvGuideRow, 0, len(rows))}
	for _, r := range rows {
		row := gen.LiveTvGuideRow{Channel: toLiveChannel(r.Channel), Programs: make([]gen.LiveTvProgram, 0, len(r.Programs))}
		for _, pr := range r.Programs {
			row.Programs = append(row.Programs, toLiveProgram(pr))
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

// GetLiveTvOnNow implements GET /livetv/on-now (empty without Live TV or the permission).
func (h *Handler) GetLiveTvOnNow(ctx context.Context, params gen.GetLiveTvOnNowParams) ([]gen.LiveTvChannel, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := liveViewer(ctx); err != nil {
		return []gen.LiveTvChannel{}, nil
	}
	limit := 20
	if v, ok := params.Limit.Get(); ok {
		limit = v
	}
	list, err := svc.OnNow(ctx, stream.Owner(p), limit)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvChannel, 0, len(list))
	for _, c := range list {
		out = append(out, toLiveChannel(c))
	}
	return out, nil
}

// GetLiveTvProgram implements GET /livetv/programs/{programId}.
func (h *Handler) GetLiveTvProgram(ctx context.Context, params gen.GetLiveTvProgramParams) (*gen.LiveTvProgram, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveViewer(ctx)
	if err != nil {
		return nil, err
	}
	var ch *uuid.UUID
	if v, ok := params.ChannelId.Get(); ok {
		ch = &v
	}
	pr, _, err := svc.ProgramOn(ctx, stream.Owner(p), params.ProgramId, ch)
	if err != nil {
		return nil, err
	}
	out := toLiveProgram(pr)
	return &out, nil
}

func toLiveRecording(r livetv.Recording) gen.LiveTvRecording {
	out := gen.LiveTvRecording{ID: r.ID, ChannelId: optUUIDP(r.ChannelID), ChannelName: optStr(r.ChannelName),
		ChannelNumber: optStr(r.ChannelNumber), ProgramId: optUUIDP(r.ProgramID), SeriesRuleId: optUUIDP(r.SeriesRuleID),
		Title: r.Title, Subtitle: optStr(r.SubTitle), Description: optStr(r.Description), Season: optIntP(r.Season),
		Episode: optIntP(r.Episode), Start: r.Start, End: r.End, PrePaddingSeconds: r.PrePadding, PostPaddingSeconds: r.PostPadding,
		Priority: r.Priority, Status: gen.LiveTvRecordingStatus(r.Status), Error: optStr(r.Error), ItemId: optUUIDP(r.ItemID),
		ImageUrl: optStr(r.ImageURL), PostProcessError: optStr(r.PostProcessError), CreatedAt: r.CreatedAt}
	if r.SizeBytes != nil {
		out.SizeBytes = gen.NewOptInt64(*r.SizeBytes)
	}
	return out
}

// ListLiveTvRecordings implements GET /livetv/recordings.
func (h *Handler) ListLiveTvRecordings(ctx context.Context, params gen.ListLiveTvRecordingsParams) ([]gen.LiveTvRecording, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveViewer(ctx); err != nil {
		return nil, err
	}
	st := make([]string, 0, len(params.Status))
	for _, s := range params.Status {
		st = append(st, string(s))
	}
	list, err := svc.Recordings(ctx, st)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvRecording, 0, len(list))
	for _, r := range list {
		out = append(out, toLiveRecording(r))
	}
	return out, nil
}

// CreateLiveTvRecording implements POST /livetv/recordings.
func (h *Handler) CreateLiveTvRecording(ctx context.Context, req *gen.LiveTvRecordingCreate) (*gen.LiveTvRecording, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveManager(ctx)
	if err != nil {
		return nil, err
	}
	in := livetv.RecordingInput{ProgramID: req.ProgramId, CreatedBy: p.User.ID}
	if v, ok := req.ChannelId.Get(); ok {
		in.ChannelID = &v
	}
	if v, ok := req.PrePaddingSeconds.Get(); ok {
		in.PrePadding = &v
	}
	if v, ok := req.PostPaddingSeconds.Get(); ok {
		in.PostPadding = &v
	}
	if v, ok := req.Priority.Get(); ok {
		in.Priority = &v
	}
	r, err := svc.CreateRecording(ctx, in)
	if err != nil {
		return nil, err
	}
	out := toLiveRecording(r)
	return &out, nil
}

// UpdateLiveTvRecording implements PATCH /livetv/recordings/{recordingId}.
func (h *Handler) UpdateLiveTvRecording(ctx context.Context, req *gen.LiveTvRecordingUpdate, params gen.UpdateLiveTvRecordingParams) (*gen.LiveTvRecording, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	var u livetv.RecordingUpdate
	if v, ok := req.PrePaddingSeconds.Get(); ok {
		u.PrePadding = &v
	}
	if v, ok := req.PostPaddingSeconds.Get(); ok {
		u.PostPadding = &v
	}
	if v, ok := req.Priority.Get(); ok {
		u.Priority = &v
	}
	r, err := svc.UpdateRecording(ctx, params.RecordingId, u)
	if err != nil {
		return nil, err
	}
	out := toLiveRecording(r)
	return &out, nil
}

// DeleteLiveTvRecording implements DELETE /livetv/recordings/{recordingId}.
func (h *Handler) DeleteLiveTvRecording(ctx context.Context, params gen.DeleteLiveTvRecordingParams) error {
	svc, err := h.tv()
	if err != nil {
		return err
	}
	if _, err := liveManager(ctx); err != nil {
		return err
	}
	return svc.DeleteRecording(ctx, params.RecordingId)
}

func toLiveRule(r livetv.SeriesRule) gen.LiveTvSeriesRule {
	return gen.LiveTvSeriesRule{ID: r.ID, Title: r.Title, ChannelId: optUUIDP(r.ChannelID), ChannelName: optStr(r.ChannelName),
		NewOnly: r.NewOnly, Priority: r.Priority, PrePaddingSeconds: optIntP(r.PrePadding), PostPaddingSeconds: optIntP(r.PostPadding),
		KeepLast: r.KeepLast, Upcoming: r.Upcoming, CreatedAt: r.CreatedAt}
}

func ruleInput(req *gen.LiveTvSeriesRuleInput, by uuid.UUID) livetv.SeriesRuleInput {
	in := livetv.SeriesRuleInput{Title: req.Title.Value, CreatedBy: by}
	if v, ok := req.ProgramId.Get(); ok {
		in.ProgramID = &v
	}
	if v, ok := req.ChannelId.Get(); ok {
		in.ChannelID = &v
	}
	if v, ok := req.NewOnly.Get(); ok {
		in.NewOnly = &v
	}
	if v, ok := req.Priority.Get(); ok {
		in.Priority = &v
	}
	if v, ok := req.PrePaddingSeconds.Get(); ok {
		in.PrePadding = &v
	}
	if v, ok := req.PostPaddingSeconds.Get(); ok {
		in.PostPadding = &v
	}
	if v, ok := req.KeepLast.Get(); ok {
		in.KeepLast = &v
	}
	return in
}

// ListLiveTvSeriesRules implements GET /livetv/series-rules.
func (h *Handler) ListLiveTvSeriesRules(ctx context.Context) ([]gen.LiveTvSeriesRule, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveViewer(ctx); err != nil {
		return nil, err
	}
	list, err := svc.SeriesRules(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvSeriesRule, 0, len(list))
	for _, r := range list {
		out = append(out, toLiveRule(r))
	}
	return out, nil
}

// CreateLiveTvSeriesRule implements POST /livetv/series-rules.
func (h *Handler) CreateLiveTvSeriesRule(ctx context.Context, req *gen.LiveTvSeriesRuleInput) (*gen.LiveTvSeriesRule, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveManager(ctx)
	if err != nil {
		return nil, err
	}
	r, err := svc.CreateSeriesRule(ctx, ruleInput(req, p.User.ID))
	if err != nil {
		return nil, err
	}
	out := toLiveRule(r)
	return &out, nil
}

// UpdateLiveTvSeriesRule implements PUT /livetv/series-rules/{ruleId}.
func (h *Handler) UpdateLiveTvSeriesRule(ctx context.Context, req *gen.LiveTvSeriesRuleInput, params gen.UpdateLiveTvSeriesRuleParams) (*gen.LiveTvSeriesRule, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveManager(ctx)
	if err != nil {
		return nil, err
	}
	r, err := svc.UpdateSeriesRule(ctx, params.RuleId, ruleInput(req, p.User.ID))
	if err != nil {
		return nil, err
	}
	out := toLiveRule(r)
	return &out, nil
}

// DeleteLiveTvSeriesRule implements DELETE /livetv/series-rules/{ruleId}.
func (h *Handler) DeleteLiveTvSeriesRule(ctx context.Context, params gen.DeleteLiveTvSeriesRuleParams) error {
	svc, err := h.tv()
	if err != nil {
		return err
	}
	if _, err := liveManager(ctx); err != nil {
		return err
	}
	return svc.DeleteSeriesRule(ctx, params.RuleId)
}

func toLiveTuner(t livetv.Tuner) gen.LiveTvTuner {
	out := gen.LiveTvTuner{ID: t.ID, Kind: gen.LiveTvTunerKind(t.Kind), Name: t.Name, URL: optStr(t.URL), FilePath: optStr(t.FilePath),
		TunerCount: t.TunerCount, UserAgent: optStr(t.UserAgent), Enabled: t.Enabled, ChannelCount: t.ChannelCount,
		DeviceId: optStr(t.DeviceID), Model: optStr(t.Model), Error: optStr(t.Error)}
	if t.LastRefreshAt != nil {
		out.LastRefreshAt = gen.NewOptDateTime(*t.LastRefreshAt)
	}
	return out
}

func tunerInput(req *gen.LiveTvTunerInput) livetv.TunerInput {
	in := livetv.TunerInput{Kind: string(req.Kind), Name: req.Name, URL: req.URL.Value, FilePath: req.FilePath.Value, UserAgent: req.UserAgent.Value}
	if v, ok := req.TunerCount.Get(); ok {
		in.TunerCount = &v
	}
	if v, ok := req.Enabled.Get(); ok {
		in.Enabled = &v
	}
	return in
}

func toTestResult(r livetv.TestResult) *gen.LiveTvTestResult {
	return &gen.LiveTvTestResult{Ok: r.OK, Message: r.Message, ChannelCount: optIntP(r.ChannelCount), ProgramCount: optIntP(r.ProgramCount),
		TunerCount: optIntP(r.TunerCount), Model: optStr(r.Model)}
}

// ListLiveTvTuners implements GET /livetv/tuners.
func (h *Handler) ListLiveTvTuners(ctx context.Context) ([]gen.LiveTvTuner, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	list, err := svc.Tuners(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvTuner, 0, len(list))
	for _, t := range list {
		out = append(out, toLiveTuner(t))
	}
	return out, nil
}

// CreateLiveTvTuner implements POST /livetv/tuners.
func (h *Handler) CreateLiveTvTuner(ctx context.Context, req *gen.LiveTvTunerInput) (*gen.LiveTvTuner, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	t, err := svc.CreateTuner(ctx, tunerInput(req))
	if err != nil {
		return nil, err
	}
	out := toLiveTuner(t)
	return &out, nil
}

// TestLiveTvTuner implements POST /livetv/tuners/test.
func (h *Handler) TestLiveTvTuner(ctx context.Context, req *gen.LiveTvTunerInput) (*gen.LiveTvTestResult, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	return toTestResult(svc.TestTuner(ctx, tunerInput(req))), nil
}

// DiscoverLiveTvTuners implements GET /livetv/tuners/discover.
func (h *Handler) DiscoverLiveTvTuners(ctx context.Context) ([]gen.LiveTvDiscoveredTuner, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	list, err := svc.Discover(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvDiscoveredTuner, 0, len(list))
	for _, d := range list {
		name := d.FriendlyName
		if name == "" {
			name = "HDHomeRun " + d.DeviceID
		}
		out = append(out, gen.LiveTvDiscoveredTuner{DeviceId: d.DeviceID, Name: name, Model: optStr(d.ModelNumber), URL: d.BaseURL,
			TunerCount: d.TunerCount, Added: d.Added})
	}
	return out, nil
}

// UpdateLiveTvTuner implements PUT /livetv/tuners/{tunerId}.
func (h *Handler) UpdateLiveTvTuner(ctx context.Context, req *gen.LiveTvTunerInput, params gen.UpdateLiveTvTunerParams) (*gen.LiveTvTuner, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	t, err := svc.UpdateTuner(ctx, params.TunerId, tunerInput(req))
	if err != nil {
		return nil, err
	}
	out := toLiveTuner(t)
	return &out, nil
}

// DeleteLiveTvTuner implements DELETE /livetv/tuners/{tunerId}.
func (h *Handler) DeleteLiveTvTuner(ctx context.Context, params gen.DeleteLiveTvTunerParams) error {
	svc, err := h.tv()
	if err != nil {
		return err
	}
	if _, err := liveManager(ctx); err != nil {
		return err
	}
	return svc.DeleteTuner(ctx, params.TunerId)
}

// RefreshLiveTvTuner implements POST /livetv/tuners/{tunerId}/refresh.
func (h *Handler) RefreshLiveTvTuner(ctx context.Context, params gen.RefreshLiveTvTunerParams) (*gen.LiveTvTuner, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	t, err := svc.RefreshTuner(ctx, params.TunerId)
	if err != nil {
		return nil, err
	}
	out := toLiveTuner(t)
	return &out, nil
}

func toLiveGuide(g livetv.GuideSource) gen.LiveTvGuideSource {
	out := gen.LiveTvGuideSource{ID: g.ID, Name: g.Name, URL: optStr(g.URL), FilePath: optStr(g.FilePath), Timezone: optStr(g.Timezone),
		RefreshHours: g.RefreshHours, Enabled: g.Enabled, ChannelCount: g.ChannelCount, ProgramCount: g.ProgramCount,
		LastResult: optStr(g.LastResult), Error: optStr(g.Error)}
	if g.LastRefreshAt != nil {
		out.LastRefreshAt = gen.NewOptDateTime(*g.LastRefreshAt)
	}
	if g.LastChangedAt != nil {
		out.LastChangedAt = gen.NewOptDateTime(*g.LastChangedAt)
	}
	return out
}

func guideInput(req *gen.LiveTvGuideSourceInput) livetv.GuideInput {
	in := livetv.GuideInput{Name: req.Name, URL: req.URL.Value, FilePath: req.FilePath.Value, Timezone: req.Timezone.Value}
	if v, ok := req.RefreshHours.Get(); ok {
		in.RefreshHours = &v
	}
	if v, ok := req.Enabled.Get(); ok {
		in.Enabled = &v
	}
	return in
}

// ListLiveTvGuides implements GET /livetv/guides.
func (h *Handler) ListLiveTvGuides(ctx context.Context) ([]gen.LiveTvGuideSource, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	list, err := svc.Guides(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.LiveTvGuideSource, 0, len(list))
	for _, g := range list {
		out = append(out, toLiveGuide(g))
	}
	return out, nil
}

// CreateLiveTvGuide implements POST /livetv/guides.
func (h *Handler) CreateLiveTvGuide(ctx context.Context, req *gen.LiveTvGuideSourceInput) (*gen.LiveTvGuideSource, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	g, err := svc.CreateGuide(ctx, guideInput(req))
	if err != nil {
		return nil, err
	}
	out := toLiveGuide(g)
	return &out, nil
}

// TestLiveTvGuide implements POST /livetv/guides/test.
func (h *Handler) TestLiveTvGuide(ctx context.Context, req *gen.LiveTvGuideSourceInput) (*gen.LiveTvTestResult, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	return toTestResult(svc.TestGuide(ctx, guideInput(req))), nil
}

// UpdateLiveTvGuide implements PUT /livetv/guides/{guideId}.
func (h *Handler) UpdateLiveTvGuide(ctx context.Context, req *gen.LiveTvGuideSourceInput, params gen.UpdateLiveTvGuideParams) (*gen.LiveTvGuideSource, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	g, err := svc.UpdateGuide(ctx, params.GuideId, guideInput(req))
	if err != nil {
		return nil, err
	}
	out := toLiveGuide(g)
	return &out, nil
}

// DeleteLiveTvGuide implements DELETE /livetv/guides/{guideId}.
func (h *Handler) DeleteLiveTvGuide(ctx context.Context, params gen.DeleteLiveTvGuideParams) error {
	svc, err := h.tv()
	if err != nil {
		return err
	}
	if _, err := liveManager(ctx); err != nil {
		return err
	}
	return svc.DeleteGuide(ctx, params.GuideId)
}

// RefreshLiveTvGuide implements POST /livetv/guides/{guideId}/refresh.
func (h *Handler) RefreshLiveTvGuide(ctx context.Context, params gen.RefreshLiveTvGuideParams) error {
	svc, err := h.tv()
	if err != nil {
		return err
	}
	if _, err := liveManager(ctx); err != nil {
		return err
	}
	return svc.QueueGuideRefresh(ctx, params.GuideId)
}

func toChannelAdmin(c livetv.Channel) gen.LiveTvChannelAdmin {
	out := gen.LiveTvChannelAdmin{Channel: toLiveChannel(c), TunerName: c.TunerName, Mapping: gen.LiveTvMappingMode(c.Mapping),
		GuideSourceId: optUUIDP(c.GuideID), GuideChannelId: optStr(c.GuideChannel), Hidden: c.Hidden,
		CategoryOverride: optCat(c.CategoryOverride), VideoCodec: optStr(c.VideoCodec), AudioCodec: optStr(c.AudioCodec)}
	return out
}

// GetLiveTvMapping implements GET /livetv/mapping.
func (h *Handler) GetLiveTvMapping(ctx context.Context) (*gen.LiveTvMapping, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	chans, gcs, err := svc.Mapping(ctx)
	if err != nil {
		return nil, err
	}
	out := &gen.LiveTvMapping{Channels: make([]gen.LiveTvChannelAdmin, 0, len(chans)), GuideChannels: make([]gen.LiveTvGuideChannel, 0, len(gcs))}
	for _, c := range chans {
		out.Channels = append(out.Channels, toChannelAdmin(c))
	}
	for _, g := range gcs {
		out.GuideChannels = append(out.GuideChannels, gen.LiveTvGuideChannel{SourceId: g.GuideID, ID: g.ID, Name: g.Name})
	}
	return out, nil
}

// UpdateLiveTvChannel implements PATCH /livetv/channels/{channelId}.
func (h *Handler) UpdateLiveTvChannel(ctx context.Context, req *gen.LiveTvChannelUpdate, params gen.UpdateLiveTvChannelParams) (*gen.LiveTvChannelAdmin, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	var u livetv.ChannelUpdate
	if v, ok := req.Mapping.Get(); ok {
		m := string(v)
		u.Mapping = &m
	}
	if v, ok := req.GuideSourceId.Get(); ok {
		u.GuideID = &v
	}
	if v, ok := req.GuideChannelId.Get(); ok {
		u.GuideChannel = &v
	}
	if v, ok := req.Hidden.Get(); ok {
		u.Hidden = &v
	}
	if v, ok := req.Category.Get(); ok {
		c := string(v)
		u.Category = &c
	}
	c, err := svc.UpdateChannel(ctx, params.ChannelId, u)
	if err != nil {
		return nil, err
	}
	out := toChannelAdmin(c)
	return &out, nil
}

func (h *Handler) toLiveSettings(ctx context.Context, svc *livetv.Service, st livetv.Settings) *gen.LiveTvSettings {
	args := st.PostProcess.Args
	if args == nil {
		args = []string{}
	}
	out := &gen.LiveTvSettings{PrePaddingSeconds: st.PrePadding, PostPaddingSeconds: st.PostPadding, TimeshiftMinutes: st.TimeshiftMinutes,
		Retention: gen.LiveTvRetention{Enabled: st.Retention.Enabled, DeleteAfterDays: st.Retention.DeleteAfterDays, KeepLast: st.Retention.KeepLast},
		PostProcess: gen.LiveTvPostProcess{Command: st.PostProcess.Command, Args: args, TimeoutMinutes: st.PostProcess.TimeoutMinutes},
		RecordingsPath: optStr(svc.RecordingsDir), RecordingsLibraryId: optUUIDP(svc.RecordingsLibrary(ctx))}
	return out
}

// GetLiveTvSettings implements GET /livetv/settings.
func (h *Handler) GetLiveTvSettings(ctx context.Context) (*gen.LiveTvSettings, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	if _, err := liveManager(ctx); err != nil {
		return nil, err
	}
	return h.toLiveSettings(ctx, svc, svc.LoadSettings(ctx)), nil
}

// UpdateLiveTvSettings implements PUT /livetv/settings.
func (h *Handler) UpdateLiveTvSettings(ctx context.Context, req *gen.LiveTvSettings) (*gen.LiveTvSettings, error) {
	svc, err := h.tv()
	if err != nil {
		return nil, err
	}
	p, err := liveManager(ctx)
	if err != nil {
		return nil, err
	}
	in := livetv.Settings{PrePadding: req.PrePaddingSeconds, PostPadding: req.PostPaddingSeconds, TimeshiftMinutes: req.TimeshiftMinutes,
		Retention:   livetv.Retention{Enabled: req.Retention.Enabled, DeleteAfterDays: req.Retention.DeleteAfterDays, KeepLast: req.Retention.KeepLast},
		PostProcess: livetv.PostProcess{Command: req.PostProcess.Command, Args: req.PostProcess.Args, TimeoutMinutes: req.PostProcess.TimeoutMinutes}}
	st, err := svc.SaveSettings(ctx, in, p.User.IsAdmin)
	if err != nil {
		return nil, err
	}
	return h.toLiveSettings(ctx, svc, st), nil
}
