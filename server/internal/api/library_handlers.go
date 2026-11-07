package api

import (
	"context"
	"errors"
	"net/http"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
)

// canSeeLibraries: admins always; others need the allLibraries permission
// (per-library access lists come with the library picker in invites/users).
func canSeeLibraries(p auth.Principal) bool { return p.User.IsAdmin || p.User.Permissions.AllLibraries }

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
	p, err := principal(ctx)
	if err != nil {
		return nil, err
	}
	out := []gen.Library{}
	if !canSeeLibraries(p) {
		return out, nil
	}
	libs, err := h.Library().List(ctx)
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
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.Library().Create(ctx, req.Name, string(req.Kind), req.Paths)
	if err != nil {
		return nil, err
	}
	out := toLibrary(l)
	return &out, nil
}

// UpdateLibrary implements PUT /libraries/{libraryId}.
func (h *Handler) UpdateLibrary(ctx context.Context, req *gen.LibraryInput, params gen.UpdateLibraryParams) (*gen.Library, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	l, err := h.Library().Update(ctx, params.LibraryId, req.Name, string(req.Kind), req.Paths)
	if err != nil {
		return nil, err
	}
	out := toLibrary(l)
	return &out, nil
}

// DeleteLibrary implements DELETE /libraries/{libraryId}.
func (h *Handler) DeleteLibrary(ctx context.Context, params gen.DeleteLibraryParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Library().Delete(ctx, params.LibraryId)
}

// ScanLibrary implements POST /libraries/{libraryId}/scan.
func (h *Handler) ScanLibrary(ctx context.Context, params gen.ScanLibraryParams) error {
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.Library().Scan(ctx, params.LibraryId)
}

// ListLibraryItems implements GET /libraries/{libraryId}/items.
func (h *Handler) ListLibraryItems(ctx context.Context, params gen.ListLibraryItemsParams) (*gen.ItemPage, error) {
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	if _, err := h.Library().Get(ctx, params.LibraryId); err != nil {
		return nil, err
	}
	items, total, err := h.Library().ListItems(ctx, params.LibraryId, string(params.Sort.Or(gen.ListLibraryItemsSortTitle)),
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
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	it, files, children, err := h.Library().GetItem(ctx, params.ItemId)
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
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	path, parent, dirs, err := h.Library().Browse(params.Path.Value)
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

func toItemSummary(it library.Item) gen.ItemSummary {
	out := gen.ItemSummary{ID: it.ID, Kind: gen.ItemKind(it.Kind), Title: it.Title, AddedAt: it.AddedAt,
		Year: optInt(it.Year), SeasonNumber: optInt(it.Season), EpisodeNumber: optInt(it.Episode), EpisodeNumberEnd: optInt(it.EpisodeEnd)}
	if it.DurationMS != nil {
		out.DurationMs = gen.NewOptInt64(*it.DurationMS)
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
