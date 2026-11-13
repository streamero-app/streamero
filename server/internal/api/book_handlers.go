package api

import (
	"context"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/books"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/stream"
)

func optUUIDPtr(o gen.OptUUID) *uuid.UUID {
	if v, ok := o.Get(); ok {
		return &v
	}
	return nil
}

func toMetadataSettings(s metadata.Settings) *gen.MetadataSettings {
	return &gen.MetadataSettings{TmdbConfigured: s.TMDbKey != "", Language: s.Language, Country: s.Country,
		OnlineBookMetadata: gen.NewOptBool(s.OnlineBookMetadata), PhotoLocation: gen.NewOptBool(s.PhotoLocation)}
}

// addMediaSummary adds the book, audiobook and photo fields of a list entry.
func addMediaSummary(out *gen.ItemSummary, it library.Item) {
	if len(it.Authors) > 0 {
		out.Authors = it.Authors
	}
	if it.SeriesName != nil {
		out.SeriesName = gen.NewOptString(*it.SeriesName)
	}
	if it.SeriesIndex != nil {
		out.SeriesIndex = gen.NewOptFloat64(*it.SeriesIndex)
	}
	if it.BookFormat != nil && (it.Kind == "book" || it.Kind == "audiobook") {
		out.BookFormat = gen.NewOptString(*it.BookFormat)
	}
	if it.Progression != nil && (it.Kind == "book" || it.Kind == "audiobook") {
		p := *it.Progression
		if it.Played {
			p = 1
		}
		out.Progression = gen.NewOptFloat64(p)
	}
	if it.TakenAt != nil {
		out.TakenAt = gen.NewOptDateTime(*it.TakenAt)
	}
	if it.Width != nil {
		out.Width = gen.NewOptInt(*it.Width)
	}
	if it.Height != nil {
		out.Height = gen.NewOptInt(*it.Height)
	}
	if it.ChildCount != nil {
		out.ChildCount = gen.NewOptInt(*it.ChildCount)
	}
}

func optFloat(p *float64) gen.OptFloat64 {
	if p == nil {
		return gen.OptFloat64{}
	}
	return gen.NewOptFloat64(*p)
}

// addMediaDetail adds what a book, audiobook or photo file says about itself.
func (h *Handler) addMediaDetail(ctx context.Context, out *gen.ItemDetail, it library.Item) error {
	switch it.Kind {
	case "book", "audiobook", "photo", "video":
	default:
		return nil
	}
	m, err := h.lib().ItemMeta(ctx, it.ID)
	if err != nil || m == nil {
		return err
	}
	if it.Kind == "photo" && m.Photo != nil {
		p := m.Photo
		d := gen.PhotoDetails{Width: optInt(it.Width), Height: optInt(it.Height), Camera: optStr(p.Camera()), Lens: optStr(p.Lens),
			ExposureTime: optStr(p.ExposureTime)}
		if p.TakenAt != nil {
			d.TakenAt = gen.NewOptDateTime(*p.TakenAt)
		}
		if p.FocalLength > 0 {
			d.FocalLength = gen.NewOptFloat64(p.FocalLength)
		}
		if p.FNumber > 0 {
			d.FNumber = gen.NewOptFloat64(p.FNumber)
		}
		if p.ISO > 0 {
			d.Iso = gen.NewOptInt(p.ISO)
		}
		// GPS stays on the server unless the administrator allows it.
		if p.Latitude != nil && p.Longitude != nil {
			if s, err := metadata.LoadSettings(ctx, h.Settings()); err == nil && s.PhotoLocation {
				d.Latitude, d.Longitude = gen.NewOptFloat64(*p.Latitude), gen.NewOptFloat64(*p.Longitude)
			}
		}
		out.Photo = gen.NewOptPhotoDetails(d)
		return nil
	}
	if it.Kind != "book" && it.Kind != "audiobook" {
		return nil
	}
	b := gen.BookDetails{Format: m.Format, Authors: m.Authors, Narrators: m.Narrators, SeriesName: optStr(m.Series),
		SeriesIndex: optFloat(m.SeriesIndex), Volume: optFloat(m.Volume), Chapter: optFloat(m.Chapter), Isbn: optStr(m.ISBN),
		Publisher: optStr(m.Publisher), Language: optStr(m.Language), Published: optStr(m.Published),
		Description: optStr(m.Description)}
	if b.Authors == nil {
		b.Authors = []string{}
	}
	if m.PageCount > 0 {
		b.PageCount = gen.NewOptInt(m.PageCount)
	}
	if m.RightToLeft {
		b.RightToLeft = gen.NewOptBool(true)
	}
	out.Book = gen.NewOptBookDetails(b)
	if !out.Overview.Set && m.Description != "" {
		out.Overview = gen.NewOptString(m.Description)
	}
	return nil
}

func (h *Handler) streamServer() (*stream.Server, error) {
	if h.lib() == nil || h.Stream == nil || h.Stream() == nil {
		return nil, errStarting
	}
	return h.Stream(), nil
}

func toReadingProgress(r library.Reading) gen.ReadingProgress {
	out := gen.ReadingProgress{Progression: r.Progression, Finished: r.Finished, Locator: optStr(r.Locator)}
	if r.Page != nil {
		out.Page = gen.NewOptInt(*r.Page)
	}
	if r.UpdatedAt != nil {
		out.UpdatedAt = gen.NewOptDateTime(*r.UpdatedAt)
	}
	return out
}

// OpenBook implements GET /items/{itemId}/read.
func (h *Handler) OpenBook(ctx context.Context, params gen.OpenBookParams) (*gen.ReadManifest, error) {
	st, err := h.streamServer()
	if err != nil {
		return nil, err
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	f, err := h.lib().ItemFile(ctx, params.ItemId)
	if err != nil {
		return nil, err
	}
	if f.Kind != "book" {
		return nil, library.ErrNotFound
	}
	m, err := h.lib().ItemMeta(ctx, params.ItemId)
	if err != nil {
		return nil, err
	}
	if m == nil {
		m = &library.MediaMeta{}
	}
	r, err := h.lib().GetReading(ctx, stream.Owner(p), params.ItemId)
	if err != nil {
		return nil, err
	}
	format := books.FormatOf(f.Path)
	out := &gen.ReadManifest{Format: format, FileUrl: st.FileURL(f.ID, f.Path), Pages: []string{}, RightToLeft: m.RightToLeft,
		Progress: toReadingProgress(r)}
	if m.PageCount > 0 {
		out.PageCount = gen.NewOptInt(m.PageCount)
	}
	if books.IsComic(format) {
		out.Pages = st.PageURLs(f.ID, m.PageCount)
	}
	return out, nil
}

// GetReadingProgress implements GET /items/{itemId}/reading-progress.
func (h *Handler) GetReadingProgress(ctx context.Context, params gen.GetReadingProgressParams) (*gen.ReadingProgress, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	r, err := h.lib().GetReading(ctx, stream.Owner(p), params.ItemId)
	if err != nil {
		return nil, err
	}
	out := toReadingProgress(r)
	return &out, nil
}

// SaveReadingProgress implements PUT /items/{itemId}/reading-progress.
func (h *Handler) SaveReadingProgress(ctx context.Context, req *gen.ReadingProgressInput, params gen.SaveReadingProgressParams) error {
	if h.lib() == nil {
		return errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return err
	}
	var page *int
	if v, ok := req.Page.Get(); ok {
		page = &v
	}
	return h.lib().SaveReading(ctx, stream.Owner(p), params.ItemId, page, req.Locator.Value, req.Progression, req.Finished.Value)
}

// OpenAudiobook implements GET /items/{itemId}/listen.
func (h *Handler) OpenAudiobook(ctx context.Context, params gen.OpenAudiobookParams) (*gen.AudiobookManifest, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	parts, chapters, err := h.lib().Audiobook(ctx, params.ItemId)
	if err != nil {
		return nil, err
	}
	it, _, _, err := h.lib().GetItem(ctx, params.ItemId, stream.Owner(p))
	if err != nil {
		return nil, err
	}
	out := &gen.AudiobookManifest{PositionMs: it.PositionMS, Parts: make([]gen.AudiobookPart, 0, len(parts)),
		Chapters: make([]gen.AudiobookChapter, 0, len(chapters))}
	for _, x := range parts {
		out.Parts = append(out.Parts, gen.AudiobookPart{FileId: x.FileID, Title: x.Title, OffsetMs: x.OffsetMS, DurationMs: x.DurationMS})
		out.DurationMs = x.OffsetMS + x.DurationMS
	}
	for _, c := range chapters {
		out.Chapters = append(out.Chapters, gen.AudiobookChapter{Title: c.Title, StartMs: c.StartMS, EndMs: c.EndMS})
	}
	return out, nil
}

// ViewPhoto implements GET /items/{itemId}/photo.
func (h *Handler) ViewPhoto(ctx context.Context, params gen.ViewPhotoParams) (*gen.PhotoView, error) {
	st, err := h.streamServer()
	if err != nil {
		return nil, err
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	f, err := h.lib().ItemFile(ctx, params.ItemId)
	if err != nil {
		return nil, err
	}
	if f.Kind != "photo" {
		return nil, library.ErrNotFound
	}
	url, mime := st.PhotoURL(f.ID, f.Path)
	return &gen.PhotoView{URL: url, OriginalUrl: gen.NewOptString(st.FileURL(f.ID, f.Path)), MimeType: mime,
		Width: optInt(f.Width), Height: optInt(f.Height)}, nil
}

func toNameCounts(ns []library.NameCount) []gen.NameCount {
	out := make([]gen.NameCount, 0, len(ns))
	for _, n := range ns {
		out = append(out, gen.NameCount{Name: n.Name, Count: n.Count, ItemId: gen.NewOptUUID(n.ItemID)})
	}
	return out
}

// ListLibraryAuthors implements GET /libraries/{libraryId}/authors.
func (h *Handler) ListLibraryAuthors(ctx context.Context, params gen.ListLibraryAuthorsParams) ([]gen.NameCount, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	ns, err := h.lib().Authors(ctx, params.LibraryId)
	if err != nil {
		return nil, err
	}
	return toNameCounts(ns), nil
}

// ListLibrarySeries implements GET /libraries/{libraryId}/series.
func (h *Handler) ListLibrarySeries(ctx context.Context, params gen.ListLibrarySeriesParams) ([]gen.NameCount, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	ns, err := h.lib().Series(ctx, params.LibraryId)
	if err != nil {
		return nil, err
	}
	return toNameCounts(ns), nil
}

// ContinueReading implements GET /home/continue-reading.
func (h *Handler) ContinueReading(ctx context.Context) ([]gen.ItemSummary, error) {
	return h.listFor(ctx, func(owner uuid.UUID) ([]library.Item, error) {
		return h.lib().ContinueReading(ctx, owner, 20)
	})
}
