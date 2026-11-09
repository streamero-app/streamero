package api

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/analyze"
	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// canSeeLibraries: admins always; others need the allLibraries permission
// (per-library access lists come with the library picker in invites/users).
func canSeeLibraries(p auth.Principal) bool { return p.User.IsAdmin || p.User.Permissions.AllLibraries }

var errStarting = &StatusError{Status: http.StatusServiceUnavailable, Code: "starting", Message: "server is starting"}

// lib returns the library service; it is installed just after start-up.
func (h *Handler) lib() *library.Service {
	if h.Library == nil {
		return nil
	}
	return h.Library()
}

func viewer(ctx context.Context) (auth.Principal, error) {
	p, err := principal(ctx)
	if err != nil {
		return p, err
	}
	if !canSeeLibraries(p) {
		return p, library.ErrNotFound
	}
	return p, nil
}

// ListLibraries implements GET /libraries.
func (h *Handler) ListLibraries(ctx context.Context) ([]gen.Library, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out := []gen.Library{}
	if !canSeeLibraries(p) {
		return out, nil
	}
	libs, err := h.lib().List(ctx)
	if err != nil {
		return nil, err
	}
	for _, l := range libs {
		out = append(out, toLibrary(l))
	}
	return out, nil
}

// CreateLibrary implements POST /libraries.
func (h *Handler) CreateLibrary(ctx context.Context, req *gen.LibraryInput) (*gen.Library, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.lib().Create(ctx, req.Name, string(req.Kind), req.Paths)
	if err != nil {
		return nil, err
	}
	out := toLibrary(l)
	return &out, nil
}

// UpdateLibrary implements PUT /libraries/{libraryId}.
func (h *Handler) UpdateLibrary(ctx context.Context, req *gen.LibraryInput, params gen.UpdateLibraryParams) (*gen.Library, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.lib().Update(ctx, params.LibraryId, req.Name, string(req.Kind), req.Paths)
	if err != nil {
		return nil, err
	}
	out := toLibrary(l)
	return &out, nil
}

// DeleteLibrary implements DELETE /libraries/{libraryId}.
func (h *Handler) DeleteLibrary(ctx context.Context, params gen.DeleteLibraryParams) error {
	if h.lib() == nil {
		return errStarting
	}
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.lib().Delete(ctx, params.LibraryId)
}

// ScanLibrary implements POST /libraries/{libraryId}/scan.
func (h *Handler) ScanLibrary(ctx context.Context, params gen.ScanLibraryParams) error {
	if h.lib() == nil {
		return errStarting
	}
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.lib().Scan(ctx, params.LibraryId)
}

// ListLibraryItems implements GET /libraries/{libraryId}/items.
func (h *Handler) ListLibraryItems(ctx context.Context, params gen.ListLibraryItemsParams) (*gen.ItemPage, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := h.lib().Get(ctx, params.LibraryId); err != nil {
		return nil, err
	}
	items, total, err := h.lib().ListItems(ctx, params.LibraryId, stream.Owner(p), string(params.Sort.Or(gen.ListLibraryItemsSortTitle)),
		params.Limit.Or(100), params.Offset.Or(0), library.ItemFilter{Genre: params.Genre.Value, Unplayed: params.Unplayed.Value,
			Favorite: params.Favorite.Value, Resumable: params.Resumable.Value, YearFrom: params.YearFrom.Value, YearTo: params.YearTo.Value,
			Letter: params.Letter.Value})
	if err != nil {
		return nil, err
	}
	out := &gen.ItemPage{Items: make([]gen.ItemSummary, 0, len(items)), Total: total}
	for _, it := range items {
		out.Items = append(out.Items, toItemSummary(it))
	}
	return out, nil
}

// GetItem implements GET /items/{itemId}.
func (h *Handler) GetItem(ctx context.Context, params gen.GetItemParams) (*gen.ItemDetail, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	it, files, children, err := h.lib().GetItem(ctx, params.ItemId, stream.Owner(p))
	if err != nil {
		return nil, err
	}
	out := &gen.ItemDetail{Item: toItemSummary(it), LibraryId: it.LibraryID, ProviderIds: gen.ItemDetailProviderIds(it.ProviderIDs),
		Files: make([]gen.MediaFile, 0, len(files)), Children: make([]gen.ItemSummary, 0, len(children))}
	if out.ProviderIds == nil {
		out.ProviderIds = gen.ItemDetailProviderIds{}
	}
	if it.ParentID != nil {
		out.ParentId = gen.NewOptUUID(*it.ParentID)
	}
	m, err := h.lib().GetMetadata(ctx, it.ID)
	if err != nil {
		return nil, err
	}
	out.Genres, out.HasBackdrop, out.HasLogo = m.Genres, m.HasBackdrop, gen.NewOptBool(m.HasLogo)
	if m.Locked {
		out.Locked = gen.NewOptBool(true)
	}
	if out.Genres == nil {
		out.Genres = []string{}
	}
	out.Cast, out.Crew = toPeople(m.Cast), toPeople(m.Crew)
	out.Studios = m.Studios
	if len(files) > 0 && h.Stream != nil && h.Stream() != nil {
		chs, tp := h.Stream().Analysis(ctx, files[0].ID)
		for _, c := range chs {
			out.Chapters = append(out.Chapters, gen.Chapter{StartMs: c.StartMS, Title: c.Title})
		}
		if tp != nil {
			out.Trickplay = gen.NewOptTrickplayInfo(trickplayInfo(files[0].ID, tp))
		}
	}
	if cs, err := h.lib().ItemCollections(ctx, it.ID); err == nil {
		for _, c := range cs {
			out.Collections = append(out.Collections, toCollection(c))
		}
	}
	setOpt := func(dst *gen.OptString, v string) {
		if v != "" {
			*dst = gen.NewOptString(v)
		}
	}
	setOpt(&out.Overview, m.Overview)
	setOpt(&out.Tagline, m.Tagline)
	setOpt(&out.OriginalTitle, m.OriginalTitle)
	setOpt(&out.OfficialRating, m.OfficialRating)
	setOpt(&out.MetadataError, m.Error)
	if m.Premiere != nil {
		out.PremiereDate = gen.NewOptDate(*m.Premiere)
	}
	for _, f := range files {
		out.Files = append(out.Files, toMediaFile(f))
	}
	for _, c := range children {
		out.Children = append(out.Children, toItemSummary(c))
	}
	return out, nil
}

// BrowseDirectories implements GET /fs/directories.
func (h *Handler) BrowseDirectories(ctx context.Context, params gen.BrowseDirectoriesParams) (*gen.DirectoryListing, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	path, parent, dirs, err := h.lib().Browse(params.Path.Value)
	if err != nil {
		return nil, err
	}
	out := &gen.DirectoryListing{Directories: dirs}
	if out.Directories == nil {
		out.Directories = []string{}
	}
	if path != "" {
		out.Path = gen.NewOptString(path)
	}
	if parent != "" {
		out.Parent = gen.NewOptString(parent)
	}
	return out, nil
}

func toLibrary(l library.Library) gen.Library {
	out := gen.Library{ID: l.ID, Name: l.Name, Kind: gen.LibraryKind(l.Kind), Paths: l.Paths, ItemCount: l.ItemCount,
		Scan: gen.ScanStatus{State: gen.ScanStatusState(l.ScanState)}}
	if l.Paths == nil {
		out.Paths = []string{}
	}
	if l.LastScanAt != nil {
		out.Scan.LastScanAt = gen.NewOptDateTime(*l.LastScanAt)
	}
	if l.ScanError != "" {
		out.Scan.Error = gen.NewOptString(l.ScanError)
	}
	out.Scan.FilesSeen = gen.NewOptInt(l.FilesSeen)
	return out
}

func optInt(p *int) gen.OptInt {
	if p == nil {
		return gen.OptInt{}
	}
	return gen.NewOptInt(*p)
}

func toPeople(ps []library.Person) []gen.Person {
	out := make([]gen.Person, 0, len(ps))
	for _, p := range ps {
		gp := gen.Person{Name: p.Name}
		if p.Role != "" {
			gp.Role = gen.NewOptString(p.Role)
		}
		if p.ImageID != nil {
			gp.ImageUrl = gen.NewOptString(fmt.Sprintf("/api/v1/people/%d/image", *p.ImageID))
		}
		out = append(out, gp)
	}
	return out
}

func toItemSummary(it library.Item) gen.ItemSummary {
	out := gen.ItemSummary{ID: it.ID, Kind: gen.ItemKind(it.Kind), Title: it.Title, AddedAt: it.AddedAt, HasPoster: it.HasPoster,
		HasBackdrop: gen.NewOptBool(it.HasBackdrop), HasLogo: gen.NewOptBool(it.HasLogo),
		Year: optInt(it.Year), SeasonNumber: optInt(it.Season), EpisodeNumber: optInt(it.Episode), EpisodeNumberEnd: optInt(it.EpisodeEnd)}
	if it.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*it.DurationMS)
	}
	if it.Rating != nil {
		out.Rating = gen.NewOptFloat64(*it.Rating)
	}
	if it.PositionMS > 0 {
		out.PositionMs = gen.NewOptInt64(it.PositionMS)
	}
	if it.Played {
		out.Played = gen.NewOptBool(true)
	}
	if it.Favorite {
		out.Favorite = gen.NewOptBool(true)
	}
	if len(it.Colors) > 0 {
		out.Colors = it.Colors
	}
	if it.SeriesID != nil {
		out.SeriesId = gen.NewOptUUID(*it.SeriesID)
		out.SeriesTitle = gen.NewOptString(it.SeriesTitle)
	}
	return out
}

func toMediaFile(f library.MediaFile) gen.MediaFile {
	out := gen.MediaFile{ID: f.ID, Path: f.Path, Size: f.Size, Container: f.Container,
		Width: optInt(f.Width), Height: optInt(f.Height)}
	if f.Label != "" {
		out.Label = gen.NewOptString(f.Label)
	}
	if f.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*f.DurationMS)
	}
	if f.VideoCodec != "" {
		out.VideoCodec = gen.NewOptString(f.VideoCodec)
	}
	if f.HDR != "" {
		out.Hdr = gen.NewOptString(f.HDR)
	}
	for _, a := range f.Audio {
		s := gen.AudioStream{Codec: a.Codec}
		if a.Language != "" {
			s.Language = gen.NewOptString(a.Language)
		}
		if a.Channels > 0 {
			s.Channels = gen.NewOptInt(a.Channels)
		}
		if a.Title != "" {
			s.Title = gen.NewOptString(a.Title)
		}
		out.Audio = append(out.Audio, s)
	}
	for _, sub := range f.Subtitles {
		s := gen.SubtitleStream{Codec: sub.Codec, External: sub.External, Forced: gen.NewOptBool(sub.Forced)}
		if sub.Language != "" {
			s.Language = gen.NewOptString(sub.Language)
		}
		out.Subtitles = append(out.Subtitles, s)
	}
	return out
}

func mapLibraryError(err error) *StatusError {
	switch {
	case errors.Is(err, library.ErrNotFound):
		return &StatusError{Status: http.StatusNotFound, Code: "not_found", Message: err.Error()}
	case errors.Is(err, library.ErrInvalidPath):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_path", Message: err.Error()}
	case errors.Is(err, library.ErrInvalidName):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_name", Message: err.Error()}
	case errors.Is(err, metadata.ErrNoKey):
		return &StatusError{Status: http.StatusConflict, Code: "tmdb_not_configured", Message: err.Error()}
	case errors.Is(err, metadata.ErrLocked):
		return &StatusError{Status: http.StatusConflict, Code: "metadata_locked", Message: err.Error()}
	case errors.Is(err, metadata.ErrInvalidKey):
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_tmdb_key", Message: err.Error()}
	case errors.Is(err, library.ErrForbidden):
		return &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: err.Error()}
	case errors.Is(err, library.ErrUnsupportedKind):
		return &StatusError{Status: http.StatusBadRequest, Code: "unsupported_kind", Message: err.Error()}
	}
	return nil
}

// RefreshLibraryMetadata implements POST /libraries/{libraryId}/refresh-metadata.
func (h *Handler) RefreshLibraryMetadata(ctx context.Context, params gen.RefreshLibraryMetadataParams) error {
	if h.lib() == nil || h.lib().EnqueueMetadata == nil {
		return errStarting
	}
	if _, err := admin(ctx); err != nil {
		return err
	}
	if _, err := h.lib().Get(ctx, params.LibraryId); err != nil {
		return err
	}
	return h.lib().EnqueueMetadata(ctx, params.LibraryId, true)
}

// GetItemImage implements GET /items/{itemId}/images/{imageType}.
func (h *Handler) GetItemImage(ctx context.Context, params gen.GetItemImageParams) (*gen.GetItemImageOKHeaders, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	// The path is built from a UUID and an enum, never from client text.
	f, err := os.Open(metadata.ImagePath(h.lib().ImageDir, params.ItemId, string(params.ImageType)))
	if err != nil {
		return nil, library.ErrNotFound
	}
	return &gen.GetItemImageOKHeaders{CacheControl: gen.NewOptString("private, max-age=86400"),
		Response: gen.GetItemImageOK{Data: f}}, nil
}

// GetPersonImage implements GET /people/{personId}/image.
func (h *Handler) GetPersonImage(ctx context.Context, params gen.GetPersonImageParams) (*gen.GetPersonImageOKHeaders, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	// The path is built from a positive integer, never from client text.
	f, err := os.Open(metadata.PersonImagePath(h.lib().ImageDir, params.PersonId))
	if err != nil {
		return nil, library.ErrNotFound
	}
	return &gen.GetPersonImageOKHeaders{CacheControl: gen.NewOptString("private, max-age=604800"),
		Response: gen.GetPersonImageOK{Data: f}}, nil
}

// GetMetadataSettings implements GET /settings/metadata.
func (h *Handler) GetMetadataSettings(ctx context.Context) (*gen.MetadataSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s, err := metadata.LoadSettings(ctx, h.Settings())
	if err != nil {
		return nil, err
	}
	return &gen.MetadataSettings{TmdbConfigured: s.TMDbKey != "", Language: s.Language, Country: s.Country}, nil
}

// UpdateMetadataSettings implements PUT /settings/metadata.
func (h *Handler) UpdateMetadataSettings(ctx context.Context, req *gen.MetadataSettingsInput) (*gen.MetadataSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	s, err := metadata.LoadSettings(ctx, h.Settings())
	if err != nil {
		return nil, err
	}
	s.Language, s.Country = req.Language, req.Country
	newKey := false
	if req.RemoveTmdbKey.Value {
		s.TMDbKey = ""
	} else if k := strings.TrimSpace(req.TmdbApiKey.Value); k != "" {
		c := h.NewTMDb(k)
		if err := c.Verify(ctx); err != nil {
			if errors.Is(err, metadata.ErrInvalidKey) {
				return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_tmdb_key", Message: err.Error()}
			}
			return nil, &StatusError{Status: http.StatusBadGateway, Code: "tmdb_unreachable", Message: err.Error()}
		}
		s.TMDbKey, newKey = k, true
	}
	if err := metadata.SaveSettings(ctx, h.Settings(), s); err != nil {
		return nil, err
	}
	// A new key: fetch metadata for everything that has none yet.
	if newKey && h.lib() != nil && h.lib().EnqueueMetadata != nil {
		if libs, err := h.lib().List(ctx); err == nil {
			for _, l := range libs {
				_ = h.lib().EnqueueMetadata(ctx, l.ID, false)
			}
		}
	}
	return &gen.MetadataSettings{TmdbConfigured: s.TMDbKey != "", Language: s.Language, Country: s.Country}, nil
}

// StartPlayback implements POST /items/{itemId}/playback.
func (h *Handler) StartPlayback(ctx context.Context, req *gen.PlaybackRequest, params gen.StartPlaybackParams) (*gen.PlaybackInfo, error) {
	if h.lib() == nil || h.Stream == nil || h.Stream() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	r := stream.Request{Device: stream.Device{Containers: req.Device.Containers, VideoCodecs: req.Device.VideoCodecs,
		AudioCodecs: req.Device.AudioCodecs, HLS: req.Device.Hls.Value, MaxHeight: req.Device.MaxHeight.Value,
		MaxBitrate: req.Device.MaxBitrate.Value}}
	if v, ok := req.FileId.Get(); ok {
		r.FileID = &v
	}
	if v, ok := req.AudioIndex.Get(); ok {
		r.AudioIndex = &v
	}
	if v, ok := req.StartMs.Get(); ok {
		r.StartMS = &v
	}
	info, err := h.Stream().Start(ctx, h.lib(), p, params.ItemId, r)
	if err == nil {
		h.trackPlayback(ctx, p, params.ItemId, info.Method, info.Reason, info.DurationMS, info.StartMS)
	}
	if err != nil {
		switch {
		case errors.Is(err, stream.ErrNotPlayable):
			return nil, &StatusError{Status: http.StatusConflict, Code: "not_playable", Message: err.Error()}
		case errors.Is(err, stream.ErrTranscodeDenied):
			return nil, &StatusError{Status: http.StatusForbidden, Code: "transcode_not_allowed", Message: err.Error()}
		case errors.Is(err, stream.ErrTranscoderFailure):
			return nil, &StatusError{Status: http.StatusBadGateway, Code: "transcode_failed", Message: err.Error()}
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
	out.Audio = toMediaFile(library.MediaFile{Audio: info.Audio}).Audio
	if out.Audio == nil {
		out.Audio = []gen.AudioStream{}
	}
	for _, s := range info.Subtitles {
		ps := gen.PlaybackSubtitle{Index: s.Index, External: s.External, URL: s.URL, Forced: gen.NewOptBool(s.Forced)}
		if s.Language != "" {
			ps.Language = gen.NewOptString(s.Language)
		}
		out.Subtitles = append(out.Subtitles, ps)
	}
	if info.SubtitleDefault >= 0 {
		out.SubtitleDefault = gen.NewOptInt(info.SubtitleDefault)
	}
	for _, c := range info.Chapters {
		out.Chapters = append(out.Chapters, gen.Chapter{StartMs: c.StartMS, Title: c.Title})
	}
	for _, sg := range info.Segments {
		out.Segments = append(out.Segments, gen.MediaSegment{Kind: gen.MediaSegmentKind(sg.Kind), StartMs: sg.StartMS, EndMs: sg.EndMS})
	}
	if tp := info.Trickplay; tp != nil {
		out.Trickplay = gen.NewOptTrickplayInfo(trickplayInfo(info.FileID, tp))
	}
	return out, nil
}

func trickplayInfo(fileID uuid.UUID, tp *stream.Trickplay) gen.TrickplayInfo {
	urls := make([]string, tp.Sheets)
	for i := range urls {
		urls[i] = fmt.Sprintf("/api/v1/trickplay/%s/%d", fileID, i)
	}
	return gen.TrickplayInfo{IntervalMs: tp.IntervalMS, Width: tp.Width, Height: tp.Height,
		Columns: tp.Columns, Rows: tp.Rows, Count: tp.Count, SheetUrls: urls}
}

// ReportProgress implements PUT /items/{itemId}/progress.
func (h *Handler) ReportProgress(ctx context.Context, req *gen.ProgressReport, params gen.ReportProgressParams) error {
	if h.lib() == nil {
		return errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return err
	}
	var dur *int64
	if v, ok := req.DurationMs.Get(); ok && v > 0 {
		dur = &v
	}
	h.trackProgress(p, params.ItemId, req.PositionMs)
	return h.lib().SaveProgress(ctx, stream.Owner(p), params.ItemId, req.PositionMs, dur, req.Played.Value)
}

// ContinueWatching implements GET /home/continue.
func (h *Handler) ContinueWatching(ctx context.Context) ([]gen.ItemSummary, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out := []gen.ItemSummary{}
	if !canSeeLibraries(p) {
		return out, nil
	}
	items, err := h.lib().ContinueWatching(ctx, stream.Owner(p), 20)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out = append(out, toItemSummary(it))
	}
	return out, nil
}

func summaries(items []library.Item) []gen.ItemSummary {
	out := make([]gen.ItemSummary, 0, len(items))
	for _, it := range items {
		out = append(out, toItemSummary(it))
	}
	return out
}

// listFor runs a per-viewer listing; viewers without library access get an empty list.
func (h *Handler) listFor(ctx context.Context, fn func(owner uuid.UUID) ([]library.Item, error)) ([]gen.ItemSummary, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	if !canSeeLibraries(p) {
		return []gen.ItemSummary{}, nil
	}
	items, err := fn(stream.Owner(p))
	if err != nil {
		return nil, err
	}
	return summaries(items), nil
}

// Search implements GET /search.
func (h *Handler) Search(ctx context.Context, params gen.SearchParams) ([]gen.ItemSummary, error) {
	return h.listFor(ctx, func(owner uuid.UUID) ([]library.Item, error) {
		return h.lib().Search(ctx, owner, params.Q, params.Limit.Or(40))
	})
}

// RecentlyAdded implements GET /home/latest.
func (h *Handler) RecentlyAdded(ctx context.Context) ([]gen.ItemSummary, error) {
	return h.listFor(ctx, func(owner uuid.UUID) ([]library.Item, error) { return h.lib().RecentlyAdded(ctx, owner, 18) })
}

// ListFavorites implements GET /home/favorites.
func (h *Handler) ListFavorites(ctx context.Context) ([]gen.ItemSummary, error) {
	return h.listFor(ctx, func(owner uuid.UUID) ([]library.Item, error) { return h.lib().Favorites(ctx, owner, 50) })
}

// ListLibraryGenres implements GET /libraries/{libraryId}/genres.
func (h *Handler) ListLibraryGenres(ctx context.Context, params gen.ListLibraryGenresParams) ([]string, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	return h.lib().Genres(ctx, params.LibraryId)
}

// UpdateUserData implements PUT /items/{itemId}/user-data.
func (h *Handler) UpdateUserData(ctx context.Context, req *gen.UserDataUpdate, params gen.UpdateUserDataParams) error {
	if h.lib() == nil {
		return errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return err
	}
	var fav, played *bool
	if v, ok := req.Favorite.Get(); ok {
		fav = &v
	}
	if v, ok := req.Played.Get(); ok {
		played = &v
	}
	return h.lib().SetUserData(ctx, stream.Owner(p), params.ItemId, fav, played)
}

// NextUp implements GET /home/next-up.
func (h *Handler) NextUp(ctx context.Context) ([]gen.ItemSummary, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out := []gen.ItemSummary{}
	if !canSeeLibraries(p) {
		return out, nil
	}
	items, err := h.lib().NextUp(ctx, stream.Owner(p), 20)
	if err != nil {
		return nil, err
	}
	for _, it := range items {
		out = append(out, toItemSummary(it))
	}
	return out, nil
}

// NextEpisode implements GET /items/{itemId}/next.
func (h *Handler) NextEpisode(ctx context.Context, params gen.NextEpisodeParams) (*gen.ItemSummary, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	it, err := h.lib().NextEpisode(ctx, stream.Owner(p), params.ItemId)
	if err != nil {
		return nil, err
	}
	s := toItemSummary(it)
	return &s, nil
}

var subtitleModes = map[string]bool{"default": true, "smart": true, "always": true, "forced": true, "none": true}

// GetPreferences implements GET /me/preferences.
func (h *Handler) GetPreferences(ctx context.Context) (*gen.PlaybackPreferences, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	prefs, err := h.lib().GetPrefs(ctx, stream.Owner(p))
	if err != nil {
		return nil, err
	}
	return toPrefs(prefs), nil
}

// UpdatePreferences implements PUT /me/preferences.
func (h *Handler) UpdatePreferences(ctx context.Context, req *gen.PlaybackPreferences) (*gen.PlaybackPreferences, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	mode := string(req.SubtitleMode)
	if !subtitleModes[mode] {
		return nil, &StatusError{Status: 400, Code: "invalid_request", Message: "unknown subtitle mode"}
	}
	autoplay := req.AutoplayNext
	prefs := library.Prefs{AudioLanguage: strings.ToLower(req.AudioLanguage.Value), SubtitleLanguage: strings.ToLower(req.SubtitleLanguage.Value),
		SubtitleMode: mode, AutoplayNext: &autoplay}
	if err := h.lib().SavePrefs(ctx, stream.Owner(p), prefs); err != nil {
		return nil, err
	}
	return toPrefs(prefs), nil
}

func toPrefs(p library.Prefs) *gen.PlaybackPreferences {
	out := &gen.PlaybackPreferences{SubtitleMode: gen.PlaybackPreferencesSubtitleMode(p.Mode()), AutoplayNext: p.Autoplay()}
	if p.AudioLanguage != "" {
		out.AudioLanguage = gen.NewOptString(p.AudioLanguage)
	}
	if p.SubtitleLanguage != "" {
		out.SubtitleLanguage = gen.NewOptString(p.SubtitleLanguage)
	}
	return out
}

// GetTrickplaySheet implements GET /trickplay/{fileId}/{sheet}.
func (h *Handler) GetTrickplaySheet(ctx context.Context, params gen.GetTrickplaySheetParams) (*gen.GetTrickplaySheetOKHeaders, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	// Built from a UUID and a bounded integer, never from client text.
	f, err := os.Open(filepath.Join(analyze.TrickplayDir(h.Paths.Cache, params.FileId), strconv.Itoa(params.Sheet)+".jpg"))
	if err != nil {
		return nil, library.ErrNotFound
	}
	return &gen.GetTrickplaySheetOKHeaders{CacheControl: gen.NewOptString("private, max-age=604800"),
		Response: gen.GetTrickplaySheetOK{Data: f}}, nil
}
