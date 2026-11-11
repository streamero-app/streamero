package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/downloads"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// downloadsService returns the downloads service (nil until ready).
func (h *Handler) downloadsService() (*downloads.Service, error) {
	d := h.Downloads.Load()
	if d == nil || h.lib() == nil {
		return nil, errStarting
	}
	return d, nil
}

func mapDownloadError(err error) *StatusError {
	switch {
	case errors.Is(err, downloads.ErrNoPermission), errors.Is(err, downloads.ErrNoTranscode):
		return &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: err.Error()}
	case errors.Is(err, downloads.ErrParental):
		return &StatusError{Status: http.StatusForbidden, Code: "parental_limit", Message: err.Error()}
	case errors.Is(err, downloads.ErrNotDownloadable):
		return &StatusError{Status: http.StatusConflict, Code: "not_downloadable", Message: err.Error()}
	case errors.Is(err, downloads.ErrBadQuality):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: err.Error()}
	}
	return nil
}

// toDownload adds the item's metadata (for the device's offline library) to a job.
func (h *Handler) toDownload(ctx context.Context, owner uuid.UUID, j downloads.Job) gen.Download {
	out := gen.Download{ID: j.ID, ItemId: j.ItemID, FileId: j.FileID, Quality: gen.DownloadQuality(j.Quality),
		State: gen.DownloadState(j.State), Progress: j.Progress, CreatedAt: j.CreatedAt,
		FileName: optString(j.FileName), Container: optString(j.Container), Error: optString(j.Error),
		Subtitles: make([]gen.DownloadSubtitle, 0, len(j.Subtitles))}
	if j.Size > 0 {
		out.SizeBytes = gen.NewOptInt64(j.Size)
	}
	if j.URL != "" {
		out.URL, out.UrlExpiresAt = gen.NewOptString(j.URL), gen.NewOptDateTime(j.URLExpires)
	}
	if j.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*j.DurationMS)
	}
	if j.AudioIndex >= 0 {
		out.AudioIndex = gen.NewOptInt(j.AudioIndex)
	}
	for _, s := range j.Subtitles {
		out.Subtitles = append(out.Subtitles, gen.DownloadSubtitle{Index: s.Index, Language: s.Language, Forced: s.Forced, URL: s.SRT, VttUrl: s.VTT})
	}
	if it, _, _, err := h.lib().GetItem(ctx, j.ItemID, owner); err == nil {
		items := []library.Item{it}
		_ = h.lib().AttachSeries(ctx, items)
		out.Item = toItemSummary(items[0])
		if m, err := h.lib().GetMetadata(ctx, it.ID); err == nil {
			out.Overview = optString(m.Overview)
		}
		if sid := items[0].SeriesID; sid != nil {
			if s, _, _, err := h.lib().GetItem(ctx, *sid, owner); err == nil {
				out.Series = gen.NewOptItemSummary(toItemSummary(s))
			}
		}
	} else {
		out.Item = gen.ItemSummary{ID: j.ItemID, Kind: gen.ItemKindMovie, Title: j.FileName, AddedAt: j.CreatedAt}
	}
	return out
}

// ListDownloads implements GET /downloads.
func (h *Handler) ListDownloads(ctx context.Context) ([]gen.Download, error) {
	d, err := h.downloadsService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	jobs, err := d.List(ctx, p)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Download, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, h.toDownload(ctx, stream.Owner(p), j))
	}
	return out, nil
}

// CreateDownloads implements POST /downloads.
func (h *Handler) CreateDownloads(ctx context.Context, req *gen.DownloadRequest) ([]gen.Download, error) {
	d, err := h.downloadsService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	r := downloads.Request{ItemID: req.ItemId, Quality: string(req.Quality), NextEpisodes: req.NextEpisodes.Value, Skip: req.SkipItemIds}
	if v, ok := req.FileId.Get(); ok {
		r.FileID = &v
	}
	if v, ok := req.AudioIndex.Get(); ok {
		r.AudioIndex = &v
	}
	jobs, err := d.Create(ctx, p, r)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Download, 0, len(jobs))
	for _, j := range jobs {
		out = append(out, h.toDownload(ctx, stream.Owner(p), j))
	}
	return out, nil
}

// GetDownload implements GET /downloads/{downloadId}.
func (h *Handler) GetDownload(ctx context.Context, params gen.GetDownloadParams) (*gen.Download, error) {
	d, err := h.downloadsService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	j, err := d.Get(ctx, p, params.DownloadId)
	if err != nil {
		return nil, err
	}
	// Access may have been lost since the job was made.
	if err := d.Allowed(ctx, p, j.ItemID); err != nil {
		_ = d.Delete(ctx, p, j.ID)
		return nil, err
	}
	out := h.toDownload(ctx, stream.Owner(p), j)
	return &out, nil
}

// DeleteDownload implements DELETE /downloads/{downloadId}.
func (h *Handler) DeleteDownload(ctx context.Context, params gen.DeleteDownloadParams) error {
	d, err := h.downloadsService()
	if err != nil {
		return err
	}
	p, err := principal(ctx)
	if err != nil {
		return err
	}
	return d.Delete(ctx, p, params.DownloadId)
}

// SyncDownloads implements POST /downloads/sync.
func (h *Handler) SyncDownloads(ctx context.Context, req *gen.OfflineSyncRequest) (*gen.OfflineSyncResponse, error) {
	d, err := h.downloadsService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	progress := make([]downloads.Progress, 0, len(req.Progress))
	for _, x := range req.Progress {
		pr := downloads.Progress{ItemID: x.ItemId, PositionMS: x.PositionMs, Played: x.Played, UpdatedAt: x.UpdatedAt}
		if v, ok := x.DurationMs.Get(); ok && v > 0 {
			pr.DurationMS = &v
		}
		progress = append(progress, pr)
	}
	res, err := d.Sync(ctx, p, req.ItemIds, progress)
	if err != nil {
		return nil, err
	}
	out := &gen.OfflineSyncResponse{Revoked: res.Revoked, Applied: res.Applied, Items: make([]gen.OfflineItemState, 0, len(res.Items))}
	for _, s := range res.Items {
		out.Items = append(out.Items, gen.OfflineItemState{ItemId: s.ItemID, PositionMs: s.PositionMS, Played: s.Played, UpdatedAt: s.UpdatedAt})
	}
	return out, nil
}

// CreateOriginalLink implements POST /items/{itemId}/original.
func (h *Handler) CreateOriginalLink(ctx context.Context, req *gen.OriginalLinkRequest, params gen.CreateOriginalLinkParams) (*gen.OriginalLink, error) {
	d, err := h.downloadsService()
	if err != nil {
		return nil, err
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	var fileID *uuid.UUID
	if v, ok := req.FileId.Get(); ok {
		fileID = &v
	}
	link, name, size, err := d.OriginalLink(ctx, p, params.ItemId, fileID)
	if err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "media.download", map[string]any{"item": params.ItemId.String()})
	return &gen.OriginalLink{URL: link, FileName: name, SizeBytes: size}, nil
}

// AdminDownloads implements GET /admin/downloads.
func (h *Handler) AdminDownloads(ctx context.Context) (*gen.DownloadAdmin, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	d, err := h.downloadsService()
	if err != nil {
		return nil, err
	}
	o, err := d.Admin(ctx)
	if err != nil {
		return nil, err
	}
	out := &gen.DownloadAdmin{Settings: toDownloadSettings(o.Settings), Running: o.Running, Queued: o.Queued, CacheBytes: o.CacheBytes,
		Transcodes: make([]gen.DownloadTranscode, 0, len(o.Transcodes))}
	for _, t := range o.Transcodes {
		out.Transcodes = append(out.Transcodes, gen.DownloadTranscode{ID: t.ID, ItemId: t.ItemID, Title: t.Title, Quality: gen.DownloadQuality(t.Quality),
			State: gen.DownloadTranscodeState(t.State), Progress: t.Progress, SizeBytes: t.Size, Error: optString(t.Error), Jobs: t.Jobs,
			Users: t.Users, CreatedAt: t.CreatedAt, LastAccessAt: t.LastAccessAt})
	}
	return out, nil
}

// DeleteDownloadTranscode implements DELETE /admin/downloads/{transcodeId}.
func (h *Handler) DeleteDownloadTranscode(ctx context.Context, params gen.DeleteDownloadTranscodeParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	d, err := h.downloadsService()
	if err != nil {
		return err
	}
	return d.AdminDelete(ctx, params.TranscodeId)
}

func toDownloadSettings(s downloads.Settings) gen.DownloadSettings {
	return gen.DownloadSettings{MaxTranscodes: s.MaxTranscodes, CacheLimitGb: s.CacheLimitGB, KeepDays: s.KeepDays}
}

// GetDownloadSettings implements GET /settings/downloads.
func (h *Handler) GetDownloadSettings(ctx context.Context) (*gen.DownloadSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s := toDownloadSettings(downloads.LoadSettings(ctx, h.Settings()))
	return &s, nil
}

// UpdateDownloadSettings implements PUT /settings/downloads.
func (h *Handler) UpdateDownloadSettings(ctx context.Context, req *gen.DownloadSettings) (*gen.DownloadSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	s := downloads.Settings{MaxTranscodes: req.MaxTranscodes, CacheLimitGB: req.CacheLimitGb, KeepDays: req.KeepDays}.Normalize()
	if err := h.Settings().Set(ctx, downloads.KeySettings, s); err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "settings.downloads", map[string]any{"maxTranscodes": s.MaxTranscodes})
	out := toDownloadSettings(s)
	return &out, nil
}
