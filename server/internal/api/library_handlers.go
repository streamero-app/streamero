package api

import (
	"context"
	"errors"
	"net/http"
	"os"
	"strings"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
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
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	if _, err := h.lib().Get(ctx, params.LibraryId); err != nil {
		return nil, err
	}
	items, total, err := h.lib().ListItems(ctx, params.LibraryId, string(params.Sort.Or(gen.ListLibraryItemsSortTitle)),
		params.Limit.Or(100), params.Offset.Or(0))
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
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	it, files, children, err := h.lib().GetItem(ctx, params.ItemId)
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
	out.Genres, out.HasBackdrop = m.Genres, m.HasBackdrop
	if out.Genres == nil {
		out.Genres = []string{}
	}
	out.Cast, out.Crew = toPeople(m.Cast), toPeople(m.Crew)
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
		out = append(out, gp)
	}
	return out
}

func toItemSummary(it library.Item) gen.ItemSummary {
	out := gen.ItemSummary{ID: it.ID, Kind: gen.ItemKind(it.Kind), Title: it.Title, AddedAt: it.AddedAt, HasPoster: it.HasPoster,
		Year: optInt(it.Year), SeasonNumber: optInt(it.Season), EpisodeNumber: optInt(it.Episode), EpisodeNumberEnd: optInt(it.EpisodeEnd)}
	if it.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*it.DurationMS)
	}
	if it.Rating != nil {
		out.Rating = gen.NewOptFloat64(*it.Rating)
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
