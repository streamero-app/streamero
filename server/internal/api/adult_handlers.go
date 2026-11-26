package api

import (
	"context"
	"errors"
	"mime"
	"net/http"
	"os"
	"path/filepath"

	"github.com/google/uuid"

	adultlib "github.com/streamero-app/streamero/server/internal/adult"
	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Adult libraries (D-053). Every handler starts at the gate: adultlib.Service.Access
// (or Open, which also wants the session PIN); a caller without access gets the
// same 404 as an unknown address.

func (h *Handler) adultService() *adultlib.Service {
	if h.Adult == nil {
		return nil
	}
	return h.Adult()
}

func mapAdultError(err error) *StatusError {
	switch {
	case errors.Is(err, adultlib.ErrLocked):
		return &StatusError{Status: http.StatusForbidden, Code: "adult_locked", Message: err.Error()}
	case errors.Is(err, adultlib.ErrInvalidPIN):
		return &StatusError{Status: http.StatusForbidden, Code: "invalid_pin", Message: err.Error()}
	case errors.Is(err, adultlib.ErrBadPIN):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_pin_format", Message: err.Error()}
	case errors.Is(err, adultlib.ErrConfirm):
		return &StatusError{Status: http.StatusBadRequest, Code: "confirmation_required", Message: err.Error()}
	case errors.Is(err, library.ErrKindChange):
		return &StatusError{Status: http.StatusBadRequest, Code: "kind_change_not_allowed", Message: err.Error()}
	case errors.Is(err, library.ErrAdultOverlap):
		return &StatusError{Status: http.StatusBadRequest, Code: "adult_overlap", Message: err.Error()}
	case errors.Is(err, adultlib.ErrBadKey):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_key", Message: err.Error()}
	case errors.Is(err, adultlib.ErrProvider):
		return &StatusError{Status: http.StatusBadGateway, Code: "metadata_unavailable", Message: err.Error()}
	}
	return nil
}

// adultAccess passes the gate without the PIN (status, unlock, preferences).
func (h *Handler) adultAccess(ctx context.Context) (*adultlib.Service, adultlib.Viewer, auth.Principal, error) {
	a := h.adultService()
	p, err := principal(ctx)
	if err != nil {
		return nil, adultlib.Viewer{}, p, err
	}
	if a == nil {
		return nil, adultlib.Viewer{}, p, adultlib.ErrNotFound
	}
	v, err := a.Access(ctx, p)
	return a, v, p, err
}

// adultOpen passes the gate including the session PIN (all content).
func (h *Handler) adultOpen(ctx context.Context) (*adultlib.Service, adultlib.Viewer, auth.Principal, error) {
	a := h.adultService()
	p, err := principal(ctx)
	if err != nil {
		return nil, adultlib.Viewer{}, p, err
	}
	if a == nil {
		return nil, adultlib.Viewer{}, p, adultlib.ErrNotFound
	}
	v, err := a.Open(ctx, p)
	return a, v, p, err
}

func toAdultStatus(st adultlib.Status) *gen.AdultStatus {
	out := &gen.AdultStatus{PinSet: st.PinSet, Unlocked: st.Unlocked, Blur: st.Blur, MetadataConfigured: st.MetadataConfigured,
		Libraries: []gen.AdultLibrary{}, HiddenTags: st.HiddenTags, HideTagged: st.HideTagged, CanDownload: st.CanDownload}
	if out.HiddenTags == nil {
		out.HiddenTags = []string{}
	}
	for _, l := range st.Libraries {
		out.Libraries = append(out.Libraries, gen.AdultLibrary{ID: l.ID, Name: l.Name, SceneCount: l.SceneCount, GalleryCount: l.GalleryCount})
	}
	return out
}

// GetAdultStatus implements GET /adult/status.
func (h *Handler) GetAdultStatus(ctx context.Context) (*gen.AdultStatus, error) {
	a, v, _, err := h.adultAccess(ctx)
	if err != nil {
		return nil, err
	}
	st, err := a.Status(ctx, v)
	if err != nil {
		return nil, err
	}
	return toAdultStatus(st), nil
}

// UnlockAdult implements POST /adult/unlock.
func (h *Handler) UnlockAdult(ctx context.Context, req *gen.AdultUnlockRequest) error {
	a, v, _, err := h.adultAccess(ctx)
	if err != nil {
		return err
	}
	return a.Unlock(ctx, v, req.Pin)
}

// LockAdult implements POST /adult/lock.
func (h *Handler) LockAdult(ctx context.Context) error {
	a, v, _, err := h.adultAccess(ctx)
	if err != nil {
		return err
	}
	a.Lock(v)
	return nil
}

// UpdateAdultPreferences implements PUT /adult/preferences.
func (h *Handler) UpdateAdultPreferences(ctx context.Context, req *gen.AdultPreferencesInput) (*gen.AdultStatus, error) {
	a, v, _, err := h.adultAccess(ctx)
	if err != nil {
		return nil, err
	}
	var in adultlib.PrefsInput
	if b, ok := req.Blur.Get(); ok {
		in.Blur = &b
	}
	if pin, ok := req.Pin.Get(); ok {
		in.PIN = &pin
	}
	in.CurrentPIN = req.CurrentPin.Value
	if req.HiddenTags != nil {
		tags := req.HiddenTags
		in.HiddenTags = &tags
	}
	if b, ok := req.HideTagged.Get(); ok {
		in.HideTagged = &b
	}
	if in.PIN == nil {
		// Blurring changes only in an open section (a set PIN is checked by SetPrefs).
		if _, err := a.Open(ctx, mustPrincipal(ctx)); err != nil {
			return nil, err
		}
	}
	if err := a.SetPrefs(ctx, v, in); err != nil {
		return nil, err
	}
	st, err := a.Status(ctx, v)
	if err != nil {
		return nil, err
	}
	return toAdultStatus(st), nil
}

func mustPrincipal(ctx context.Context) auth.Principal {
	p, _ := principal(ctx)
	return p
}

func toAdultScene(sc adultlib.Scene) gen.AdultScene {
	out := gen.AdultScene{ID: sc.ID, LibraryId: sc.LibraryID, Kind: gen.AdultSceneKind(sc.Kind), Title: sc.Title,
		Performers: []gen.AdultRef{}, Tags: sc.Tags, HasPoster: sc.HasPoster, HasBackdrop: sc.HasBackdrop,
		PositionMs: sc.PositionMS, Played: sc.Played, AddedAt: sc.AddedAt}
	if sc.Date != nil {
		out.Date = gen.NewOptDate(*sc.Date)
	}
	if sc.Year != nil {
		out.Year = gen.NewOptInt(*sc.Year)
	}
	if sc.Studio != nil {
		out.Studio = gen.NewOptAdultRef(gen.AdultRef{ID: sc.Studio.ID, Name: sc.Studio.Name})
	}
	for _, p := range sc.Performers {
		out.Performers = append(out.Performers, gen.AdultRef{ID: p.ID, Name: p.Name})
	}
	if sc.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*sc.DurationMS)
	}
	if sc.Height != nil {
		out.Height = gen.NewOptInt(*sc.Height)
	}
	return out
}

func toAdultSceneDetail(sc adultlib.Scene) *gen.AdultSceneDetail {
	b := toAdultScene(sc)
	out := &gen.AdultSceneDetail{ID: b.ID, LibraryId: b.LibraryId, Kind: b.Kind, Title: b.Title, Date: b.Date, Year: b.Year,
		Studio: b.Studio, Performers: b.Performers, Tags: b.Tags, DurationMs: b.DurationMs, Height: b.Height, HasPoster: b.HasPoster,
		HasBackdrop: b.HasBackdrop, PositionMs: b.PositionMs, Played: b.Played, AddedAt: b.AddedAt,
		Description: optString(sc.Description), URL: optString(sc.URL), MetadataError: optString(sc.MetadataError),
		Files: []gen.AdultFile{}, ProviderIds: gen.AdultSceneDetailProviderIds{}, Galleries: []gen.AdultGallery{}}
	for _, g := range sc.Galleries {
		out.Galleries = append(out.Galleries, toAdultGallery(g))
	}
	for k, v := range sc.ProviderIDs {
		out.ProviderIds[k] = v
	}
	for _, f := range sc.Files {
		// The name only: folders of the media server are not shown to viewers.
		gf := gen.AdultFile{ID: f.ID, Name: filepath.Base(f.Path), Size: f.Size, Container: f.Container, VideoCodec: optString(f.Codec)}
		if f.Width != nil {
			gf.Width = gen.NewOptInt(*f.Width)
		}
		if f.Height != nil {
			gf.Height = gen.NewOptInt(*f.Height)
		}
		if f.DurationMS != nil {
			gf.DurationMs = gen.NewOptInt64(*f.DurationMS)
		}
		out.Files = append(out.Files, gf)
	}
	return out
}

func optUUID(v gen.OptUUID) *uuid.UUID {
	if id, ok := v.Get(); ok {
		return &id
	}
	return nil
}

// ListAdultScenes implements GET /adult/scenes.
func (h *Handler) ListAdultScenes(ctx context.Context, params gen.ListAdultScenesParams) (*gen.AdultScenePage, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	f := adultlib.Filter{LibraryID: optUUID(params.LibraryId), StudioID: optUUID(params.StudioId), PerformerID: optUUID(params.PerformerId),
		Kind: string(params.Kind.Value), Query: params.Q.Value, Tag: params.Tag.Value, Status: string(params.Status.Value),
		Sort: string(params.Sort.Value), Offset: params.Offset.Or(0), Limit: params.Limit.Or(60),
		FavoritePerformers: params.FavoritePerformers.Value}
	if y, ok := params.Year.Get(); ok {
		f.Year = &y
	}
	if f.Status == "all" {
		f.Status = ""
	}
	if !params.Sort.Set {
		f.Sort = ""
	}
	list, total, err := a.Scenes(ctx, v, f)
	if err != nil {
		return nil, err
	}
	out := &gen.AdultScenePage{Items: []gen.AdultScene{}, Total: total}
	for _, sc := range list {
		out.Items = append(out.Items, toAdultScene(sc))
	}
	return out, nil
}

// GetAdultScene implements GET /adult/scenes/{sceneId}.
func (h *Handler) GetAdultScene(ctx context.Context, params gen.GetAdultSceneParams) (*gen.AdultSceneDetail, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	sc, err := a.Scene(ctx, v, params.SceneId)
	if err != nil {
		return nil, err
	}
	return toAdultSceneDetail(sc), nil
}

// RefreshAdultScene implements POST /adult/scenes/{sceneId}/refresh.
func (h *Handler) RefreshAdultScene(ctx context.Context, params gen.RefreshAdultSceneParams) (*gen.AdultSceneDetail, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	if !v.IsAdmin {
		return nil, errForbidden
	}
	if _, err := a.Scene(ctx, v, params.SceneId); err != nil {
		return nil, err
	}
	if err := a.RefreshScene(ctx, params.SceneId); err != nil {
		return nil, err
	}
	sc, err := a.Scene(ctx, v, params.SceneId)
	if err != nil {
		return nil, err
	}
	return toAdultSceneDetail(sc), nil
}

// StartAdultPlayback implements POST /adult/scenes/{sceneId}/playback. Nothing is
// reported to the activity dashboard, notifications or watch-together.
func (h *Handler) StartAdultPlayback(ctx context.Context, req *gen.PlaybackRequest, params gen.StartAdultPlaybackParams) (*gen.PlaybackInfo, error) {
	a, v, p, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	if h.Stream == nil || h.Stream() == nil {
		return nil, errStarting
	}
	if _, err := a.Scene(ctx, v, params.SceneId); err != nil {
		return nil, err
	}
	files, err := a.MediaFiles(ctx, params.SceneId)
	if err != nil {
		return nil, err
	}
	r := stream.Request{Device: stream.Device{Containers: req.Device.Containers, VideoCodecs: req.Device.VideoCodecs,
		AudioCodecs: req.Device.AudioCodecs, HLS: req.Device.Hls.Value, MaxHeight: req.Device.MaxHeight.Value,
		MaxBitrate: req.Device.MaxBitrate.Value, SubtitleCodecs: req.Device.SubtitleCodecs}}
	if v, ok := req.FileId.Get(); ok {
		r.FileID = &v
	}
	if v, ok := req.AudioIndex.Get(); ok {
		r.AudioIndex = &v
	}
	if v, ok := req.StartMs.Get(); ok {
		r.StartMS = &v
	}
	info, err := h.Stream().StartAdult(ctx, p, files, a.Position(ctx, v, params.SceneId), r)
	if err != nil {
		switch {
		case errors.Is(err, stream.ErrNotPlayable):
			return nil, &StatusError{Status: http.StatusConflict, Code: "not_playable", Message: err.Error()}
		case errors.Is(err, stream.ErrTranscodeDenied):
			return nil, &StatusError{Status: http.StatusForbidden, Code: "transcode_not_allowed", Message: err.Error()}
		case errors.Is(err, stream.ErrBusy):
			return nil, &StatusError{Status: http.StatusServiceUnavailable, Code: "transcoder_busy", Message: err.Error()}
		case errors.Is(err, stream.ErrTranscoderFailure):
			return nil, &StatusError{Status: http.StatusBadGateway, Code: "transcode_failed", Message: "the stream could not be started"}
		case errors.Is(err, os.ErrNotExist):
			return nil, &StatusError{Status: http.StatusNotFound, Code: "file_missing", Message: "the file is no longer on disk; rescan the library"}
		}
		return nil, err
	}
	out := &gen.PlaybackInfo{FileId: info.FileID, Method: gen.PlaybackInfoMethod(info.Method), URL: info.URL,
		StartMs: info.StartMS, AudioIndex: gen.NewOptInt(info.AudioIndex), Audio: []gen.AudioStream{}, Subtitles: []gen.PlaybackSubtitle{}}
	if info.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*info.DurationMS)
	}
	if info.Reason != "" {
		out.Reason = gen.NewOptString(info.Reason)
	}
	if au := toMediaFile(library.MediaFile{Audio: info.Audio}).Audio; au != nil {
		out.Audio = au
	}
	return out, nil
}

// ReportAdultProgress implements PUT /adult/scenes/{sceneId}/progress.
func (h *Handler) ReportAdultProgress(ctx context.Context, req *gen.ProgressReport, params gen.ReportAdultProgressParams) error {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return err
	}
	var dur *int64
	if d, ok := req.DurationMs.Get(); ok {
		dur = &d
	}
	return a.SaveProgress(ctx, v, params.SceneId, req.PositionMs, dur, req.Played.Value)
}

func toAdultStudio(s adultlib.Studio) gen.AdultStudio {
	return gen.AdultStudio{ID: s.ID, Name: s.Name, SceneCount: s.SceneCount, GalleryCount: s.GalleryCount, HasImage: s.HasImage, URL: optString(s.URL),
		Parent: optString(s.Parent)}
}

func toAdultPerformer(p adultlib.Performer) gen.AdultPerformer {
	return gen.AdultPerformer{ID: p.ID, Name: p.Name, SceneCount: p.SceneCount, GalleryCount: p.GalleryCount, Favorite: p.Favorite,
		HasImage: p.HasImage, Aliases: p.Aliases,
		Gender: optString(p.Gender), Birthdate: optString(p.Birthdate), Country: optString(p.Country), Description: optString(p.Description)}
}

// ListAdultStudios implements GET /adult/studios.
func (h *Handler) ListAdultStudios(ctx context.Context, params gen.ListAdultStudiosParams) (*gen.AdultStudioPage, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	list, total, err := a.Studios(ctx, v, adultlib.NamedFilter{Query: params.Q.Value, Sort: string(params.Sort.Value),
		Offset: params.Offset.Or(0), Limit: params.Limit.Or(100)})
	if err != nil {
		return nil, err
	}
	out := &gen.AdultStudioPage{Items: []gen.AdultStudio{}, Total: total}
	for _, s := range list {
		out.Items = append(out.Items, toAdultStudio(s))
	}
	return out, nil
}

// GetAdultStudio implements GET /adult/studios/{studioId}.
func (h *Handler) GetAdultStudio(ctx context.Context, params gen.GetAdultStudioParams) (*gen.AdultStudio, error) {
	a, _, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	s, err := a.Studio(ctx, params.StudioId)
	if err != nil {
		return nil, err
	}
	out := toAdultStudio(s)
	return &out, nil
}

// ListAdultPerformers implements GET /adult/performers.
func (h *Handler) ListAdultPerformers(ctx context.Context, params gen.ListAdultPerformersParams) (*gen.AdultPerformerPage, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	list, total, err := a.Performers(ctx, v, adultlib.NamedFilter{Query: params.Q.Value, Sort: string(params.Sort.Value),
		Favorites: params.Favorites.Value, Offset: params.Offset.Or(0), Limit: params.Limit.Or(100)})
	if err != nil {
		return nil, err
	}
	out := &gen.AdultPerformerPage{Items: []gen.AdultPerformer{}, Total: total}
	for _, p := range list {
		out.Items = append(out.Items, toAdultPerformer(p))
	}
	return out, nil
}

// GetAdultPerformer implements GET /adult/performers/{performerId}.
func (h *Handler) GetAdultPerformer(ctx context.Context, params gen.GetAdultPerformerParams) (*gen.AdultPerformer, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	p, err := a.Performer(ctx, v, params.PerformerId)
	if err != nil {
		return nil, err
	}
	out := toAdultPerformer(p)
	return &out, nil
}

// GetAdultImage implements GET /adult/images/{owner}/{id}/{imageType}.
func (h *Handler) GetAdultImage(ctx context.Context, params gen.GetAdultImageParams) (*gen.GetAdultImageOKHeaders, error) {
	a, _, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	// Built from an enum, a UUID and an enum only.
	path := a.ImagePath(string(params.Owner), params.ID, string(params.ImageType))
	if path == "" {
		return nil, adultlib.ErrNotFound
	}
	f, err := os.Open(path) // #nosec G304 -- see above
	if err != nil {
		return nil, adultlib.ErrNotFound
	}
	return &gen.GetAdultImageOKHeaders{CacheControl: gen.NewOptString("private, no-store"), Response: gen.GetAdultImageOK{Data: f}}, nil
}

// GetAdultSettings implements GET /adult/settings (admin).
func (h *Handler) GetAdultSettings(ctx context.Context) (*gen.AdultSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	a := h.adultService()
	if a == nil {
		return nil, errStarting
	}
	k, err := a.LoadKeys(ctx)
	if err != nil {
		return nil, err
	}
	return &gen.AdultSettings{TpdbConfigured: k.TPDB != "", StashdbConfigured: k.StashDB != ""}, nil
}

// UpdateAdultSettings implements PUT /adult/settings (admin). Keys are checked,
// stored encrypted and never returned.
func (h *Handler) UpdateAdultSettings(ctx context.Context, req *gen.AdultSettingsInput) (*gen.AdultSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	a := h.adultService()
	if a == nil {
		return nil, errStarting
	}
	k, err := a.SaveKeys(ctx, adultlib.KeysInput{TPDB: req.TpdbApiKey.Value, StashDB: req.StashdbApiKey.Value,
		ClearTPDB: req.ClearTpdb.Value, ClearStashDB: req.ClearStashdb.Value})
	if err != nil {
		return nil, err
	}
	if (req.TpdbApiKey.Value != "" || req.StashdbApiKey.Value != "") && h.EnqueueAdult != nil && h.lib() != nil {
		if libs, err := h.lib().ListAll(ctx); err == nil {
			for _, l := range libs {
				if l.Kind == library.KindAdult {
					_ = h.EnqueueAdult(ctx, l.ID, false)
				}
			}
		}
	}
	return &gen.AdultSettings{TpdbConfigured: k.TPDB != "", StashdbConfigured: k.StashDB != ""}, nil
}

// SetUserAdultAccess implements PUT /users/{userId}/adult-access (admin).
func (h *Handler) SetUserAdultAccess(ctx context.Context, req *gen.AdultAccessInput, params gen.SetUserAdultAccessParams) (*gen.User, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	a := h.adultService()
	if a == nil {
		return nil, errStarting
	}
	if err := a.SetAccess(ctx, p, params.UserId, req.Enabled, req.Confirm.Value); err != nil {
		return nil, err
	}
	users, err := h.Auth().ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.ID == params.UserId {
			out := toUser(u)
			out.AdultAccess = gen.NewOptBool(req.Enabled)
			return &out, nil
		}
	}
	return nil, adultlib.ErrNotFound
}

// AddAdultFavoritePerformer implements PUT /adult/performers/{performerId}/favorite.
func (h *Handler) AddAdultFavoritePerformer(ctx context.Context, params gen.AddAdultFavoritePerformerParams) error {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return err
	}
	return a.SetFavorite(ctx, v, params.PerformerId, true)
}

// RemoveAdultFavoritePerformer implements DELETE /adult/performers/{performerId}/favorite.
func (h *Handler) RemoveAdultFavoritePerformer(ctx context.Context, params gen.RemoveAdultFavoritePerformerParams) error {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return err
	}
	return a.SetFavorite(ctx, v, params.PerformerId, false)
}

func toAdultGallery(g adultlib.Gallery) gen.AdultGallery {
	out := gen.AdultGallery{ID: g.ID, LibraryId: g.LibraryID, Title: g.Title, Performers: []gen.AdultRef{}, Tags: g.Tags,
		ImageCount: g.ImageCount, AddedAt: g.AddedAt}
	if out.Tags == nil {
		out.Tags = []string{}
	}
	if g.Date != nil {
		out.Date = gen.NewOptDate(*g.Date)
	}
	if g.Year != nil {
		out.Year = gen.NewOptInt(*g.Year)
	}
	if g.Studio != nil {
		out.Studio = gen.NewOptAdultRef(gen.AdultRef{ID: g.Studio.ID, Name: g.Studio.Name})
	}
	if g.Scene != nil {
		out.Scene = gen.NewOptAdultRef(gen.AdultRef{ID: g.Scene.ID, Name: g.Scene.Name})
	}
	if g.CoverImageID != nil {
		out.CoverImageId = gen.NewOptUUID(*g.CoverImageID)
	}
	for _, p := range g.Performers {
		out.Performers = append(out.Performers, gen.AdultRef{ID: p.ID, Name: p.Name})
	}
	return out
}

func toAdultScenes(list []adultlib.Scene) []gen.AdultScene {
	out := []gen.AdultScene{}
	for _, sc := range list {
		out = append(out, toAdultScene(sc))
	}
	return out
}

func toAdultGalleries(list []adultlib.Gallery) []gen.AdultGallery {
	out := []gen.AdultGallery{}
	for _, g := range list {
		out = append(out, toAdultGallery(g))
	}
	return out
}

// ListAdultGalleries implements GET /adult/galleries.
func (h *Handler) ListAdultGalleries(ctx context.Context, params gen.ListAdultGalleriesParams) (*gen.AdultGalleryPage, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	f := adultlib.GalleryFilter{LibraryID: optUUID(params.LibraryId), StudioID: optUUID(params.StudioId), PerformerID: optUUID(params.PerformerId),
		SceneID: optUUID(params.SceneId), Query: params.Q.Value, Tag: params.Tag.Value, Sort: string(params.Sort.Value),
		Offset: params.Offset.Or(0), Limit: params.Limit.Or(60)}
	if y, ok := params.Year.Get(); ok {
		f.Year = &y
	}
	list, total, err := a.Galleries(ctx, v, f)
	if err != nil {
		return nil, err
	}
	return &gen.AdultGalleryPage{Items: toAdultGalleries(list), Total: total}, nil
}

// GetAdultGallery implements GET /adult/galleries/{galleryId}.
func (h *Handler) GetAdultGallery(ctx context.Context, params gen.GetAdultGalleryParams) (*gen.AdultGalleryDetail, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	g, err := a.Gallery(ctx, v, params.GalleryId)
	if err != nil {
		return nil, err
	}
	b := toAdultGallery(g)
	out := &gen.AdultGalleryDetail{ID: b.ID, LibraryId: b.LibraryId, Title: b.Title, Date: b.Date, Year: b.Year, Studio: b.Studio,
		Scene: b.Scene, Performers: b.Performers, Tags: b.Tags, ImageCount: b.ImageCount, CoverImageId: b.CoverImageId, AddedAt: b.AddedAt,
		Images: []gen.AdultGalleryImage{}}
	for _, im := range g.Images {
		gi := gen.AdultGalleryImage{ID: im.ID, Position: im.Position, Name: im.Name}
		if im.Width != nil {
			gi.Width = gen.NewOptInt(*im.Width)
		}
		if im.Height != nil {
			gi.Height = gen.NewOptInt(*im.Height)
		}
		out.Images = append(out.Images, gi)
	}
	return out, nil
}

// GetAdultGalleryImage implements GET /adult/gallery-images/{imageId}/{variant}:
// through the gate only, never cached by the browser or a proxy; the original
// file only with the download permission.
func (h *Handler) GetAdultGalleryImage(ctx context.Context, params gen.GetAdultGalleryImageParams) (gen.GetAdultGalleryImageRes, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	variant := string(params.Variant)
	if variant == "download" && !v.CanDownload {
		return nil, errForbidden
	}
	pic, err := a.Image(ctx, params.ImageId, variant)
	if err != nil {
		return nil, err
	}
	cache := gen.NewOptString("private, no-store")
	disp := gen.OptString{}
	if variant == "download" {
		disp = gen.NewOptString(mime.FormatMediaType("attachment", map[string]string{"filename": pic.Name}))
	}
	switch pic.ContentType {
	case "image/jpeg":
		return &gen.GetAdultGalleryImageOKImageJpegHeaders{CacheControl: cache, ContentDisposition: disp,
			Response: gen.GetAdultGalleryImageOKImageJpeg{Data: pic.Data}}, nil
	case "image/png":
		return &gen.GetAdultGalleryImageOKImagePNGHeaders{CacheControl: cache, ContentDisposition: disp,
			Response: gen.GetAdultGalleryImageOKImagePNG{Data: pic.Data}}, nil
	case "image/webp":
		return &gen.GetAdultGalleryImageOKImageWEBPHeaders{CacheControl: cache, ContentDisposition: disp,
			Response: gen.GetAdultGalleryImageOKImageWEBP{Data: pic.Data}}, nil
	case "image/gif":
		return &gen.GetAdultGalleryImageOKImageGIFHeaders{CacheControl: cache, ContentDisposition: disp,
			Response: gen.GetAdultGalleryImageOKImageGIF{Data: pic.Data}}, nil
	}
	return &gen.GetAdultGalleryImageOKApplicationOctetStreamHeaders{CacheControl: cache, ContentDisposition: disp,
		Response: gen.GetAdultGalleryImageOKApplicationOctetStream{Data: pic.Data}}, nil
}

// ListAdultTags implements GET /adult/tags.
func (h *Handler) ListAdultTags(ctx context.Context, params gen.ListAdultTagsParams) (*gen.AdultTagPage, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	list, total, err := a.Tags(ctx, v, adultlib.TagFilter{Query: params.Q.Value, Sort: string(params.Sort.Value),
		IncludeHidden: params.IncludeHidden.Value, Offset: params.Offset.Or(0), Limit: params.Limit.Or(200)})
	if err != nil {
		return nil, err
	}
	return &gen.AdultTagPage{Items: toAdultTags(list), Total: total}, nil
}

func toAdultTags(list []adultlib.Tag) []gen.AdultTag {
	out := []gen.AdultTag{}
	for _, t := range list {
		out = append(out, gen.AdultTag{Name: t.Name, SceneCount: t.SceneCount, GalleryCount: t.GalleryCount, Hidden: t.Hidden})
	}
	return out
}

// GetAdultHome implements GET /adult/home.
func (h *Handler) GetAdultHome(ctx context.Context) (*gen.AdultHome, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	home, err := a.Home(ctx, v)
	if err != nil {
		return nil, err
	}
	return &gen.AdultHome{Continue: toAdultScenes(home.Continue), RecentlyAdded: toAdultScenes(home.RecentlyAdded),
		RecentlyReleased: toAdultScenes(home.RecentlyReleased), FavoritePerformers: toAdultScenes(home.FavoritePerformers),
		RecentGalleries: toAdultGalleries(home.RecentGalleries)}, nil
}

// SearchAdult implements GET /adult/search.
func (h *Handler) SearchAdult(ctx context.Context, params gen.SearchAdultParams) (*gen.AdultSearchResult, error) {
	a, v, _, err := h.adultOpen(ctx)
	if err != nil {
		return nil, err
	}
	r, err := a.Search(ctx, v, params.Q, params.Limit.Or(12))
	if err != nil {
		return nil, err
	}
	out := &gen.AdultSearchResult{Scenes: toAdultScenes(r.Scenes), Galleries: toAdultGalleries(r.Galleries),
		Performers: []gen.AdultPerformer{}, Studios: []gen.AdultStudio{}, Tags: toAdultTags(r.Tags)}
	for _, p := range r.Performers {
		out.Performers = append(out.Performers, toAdultPerformer(p))
	}
	for _, st := range r.Studios {
		out.Studios = append(out.Studios, toAdultStudio(st))
	}
	return out, nil
}
