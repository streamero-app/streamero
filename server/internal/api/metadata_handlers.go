package api

import (
	"context"
	"errors"
	"io"
	"log/slog"

	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
)

// refresher builds a metadata refresher for one-off admin actions in the API process.
func (h *Handler) refresher() *metadata.Refresher {
	r := &metadata.Refresher{Pool: h.lib().Pool, Settings: h.Settings(), ImageDir: h.lib().ImageDir, Log: discardLog()}
	if h.NewTMDb != nil {
		r.NewClient = h.NewTMDb
	}
	return r
}

func notFoundRows(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return library.ErrNotFound
	}
	return err
}

// IdentifySearch implements GET /items/{itemId}/identify.
func (h *Handler) IdentifySearch(ctx context.Context, params gen.IdentifySearchParams) ([]gen.IdentifyResult, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	cs, err := h.refresher().Candidates(ctx, params.ItemId, params.Query.Value, params.Year.Value)
	if err != nil {
		return nil, notFoundRows(err)
	}
	out := make([]gen.IdentifyResult, 0, len(cs))
	for _, c := range cs {
		r := gen.IdentifyResult{TmdbId: c.ID, Title: c.Title}
		if c.OriginalTitle != "" && c.OriginalTitle != c.Title {
			r.OriginalTitle = gen.NewOptString(c.OriginalTitle)
		}
		if c.Year > 0 {
			r.Year = gen.NewOptInt(c.Year)
		}
		if c.Overview != "" {
			r.Overview = gen.NewOptString(c.Overview)
		}
		if c.PosterPath != "" {
			r.PosterUrl = gen.NewOptString("https://image.tmdb.org/t/p/w185" + c.PosterPath)
		}
		out = append(out, r)
	}
	return out, nil
}

// IdentifyApply implements POST /items/{itemId}/identify.
func (h *Handler) IdentifyApply(ctx context.Context, req *gen.IdentifyApplyReq, params gen.IdentifyApplyParams) error {
	if h.lib() == nil {
		return errStarting
	}
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	if err := h.refresher().Identify(ctx, params.ItemId, req.TmdbId); err != nil {
		return notFoundRows(err)
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "item.identified", map[string]any{"item_id": params.ItemId, "tmdb_id": req.TmdbId})
	return nil
}

// RefreshItem implements POST /items/{itemId}/refresh.
func (h *Handler) RefreshItem(ctx context.Context, params gen.RefreshItemParams) error {
	if h.lib() == nil {
		return errStarting
	}
	if _, err := admin(ctx); err != nil {
		return err
	}
	return notFoundRows(h.refresher().RefreshItem(ctx, params.ItemId))
}

// EditMetadata implements PUT /items/{itemId}/metadata.
func (h *Handler) EditMetadata(ctx context.Context, req *gen.MetadataEdit, params gen.EditMetadataParams) error {
	if h.lib() == nil {
		return errStarting
	}
	p, err := admin(ctx)
	if err != nil {
		return err
	}
	e := library.MetadataEdit{Title: req.Title, OriginalTitle: req.OriginalTitle.Value, Overview: req.Overview.Value,
		Tagline: req.Tagline.Value, OfficialRating: req.OfficialRating.Value, Genres: req.Genres, Locked: req.Locked}
	if v, ok := req.Year.Get(); ok {
		e.Year = &v
	}
	if v, ok := req.Rating.Get(); ok {
		e.Rating = &v
	}
	if err := h.lib().EditMetadata(ctx, params.ItemId, e); err != nil {
		return err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "item.edited", map[string]any{"item_id": params.ItemId, "title": req.Title})
	return nil
}

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }
