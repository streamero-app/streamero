package api

import (
	"context"
	"os"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/stream"
)

func toCollection(c library.Collection) gen.Collection {
	out := gen.Collection{ID: c.ID, Name: c.Name, ItemCount: c.ItemCount, HasPoster: c.HasPoster, HasBackdrop: c.HasBackdrop, Auto: c.TMDbID != nil}
	if c.Overview != "" {
		out.Overview = gen.NewOptString(c.Overview)
	}
	return out
}

// ListCollections implements GET /collections.
func (h *Handler) ListCollections(ctx context.Context) ([]gen.Collection, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	cs, err := h.lib().Collections(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Collection, 0, len(cs))
	for _, c := range cs {
		out = append(out, toCollection(c))
	}
	return out, nil
}

// GetCollection implements GET /collections/{collectionId}.
func (h *Handler) GetCollection(ctx context.Context, params gen.GetCollectionParams) (*gen.CollectionDetail, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	c, items, err := h.lib().GetCollection(ctx, params.CollectionId, stream.Owner(p))
	if err != nil {
		return nil, err
	}
	return &gen.CollectionDetail{Collection: toCollection(c), Items: summaries(items)}, nil
}

// CreateCollection implements POST /collections.
func (h *Handler) CreateCollection(ctx context.Context, req *gen.CollectionInput) (*gen.Collection, error) {
	return h.saveCollection(ctx, uuid.Nil, req)
}

// UpdateCollection implements PUT /collections/{collectionId}.
func (h *Handler) UpdateCollection(ctx context.Context, req *gen.CollectionInput, params gen.UpdateCollectionParams) (*gen.Collection, error) {
	return h.saveCollection(ctx, params.CollectionId, req)
}

func (h *Handler) saveCollection(ctx context.Context, id uuid.UUID, req *gen.CollectionInput) (*gen.Collection, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	c, err := h.lib().SaveCollection(ctx, id, req.Name, req.Overview.Value, req.ItemIds)
	if err != nil {
		return nil, err
	}
	out := toCollection(c)
	return &out, nil
}

// DeleteCollection implements DELETE /collections/{collectionId}.
func (h *Handler) DeleteCollection(ctx context.Context, params gen.DeleteCollectionParams) error {
	if h.lib() == nil {
		return errStarting
	}
	if _, err := admin(ctx); err != nil {
		return err
	}
	return h.lib().DeleteCollection(ctx, params.CollectionId)
}

// GetCollectionImage implements GET /collections/{collectionId}/images/{imageType}.
func (h *Handler) GetCollectionImage(ctx context.Context, params gen.GetCollectionImageParams) (*gen.GetCollectionImageOKHeaders, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	if _, err := viewer(ctx); err != nil {
		return nil, err
	}
	// Built from a UUID and an enum, never from client text.
	f, err := os.Open(metadata.CollectionImagePath(h.lib().ImageDir, params.CollectionId, string(params.ImageType)))
	if err != nil {
		return nil, library.ErrNotFound
	}
	return &gen.GetCollectionImageOKHeaders{CacheControl: gen.NewOptString("private, max-age=86400"),
		Response: gen.GetCollectionImageOK{Data: f}}, nil
}

func toPlaylist(p library.Playlist, owner uuid.UUID) gen.Playlist {
	return gen.Playlist{ID: p.ID, Name: p.Name, ItemCount: p.ItemCount, DurationMs: p.DurationMS, Public: p.Public,
		Mine: p.OwnerID == owner, CoverItemIds: p.Covers}
}

// ListPlaylists implements GET /playlists.
func (h *Handler) ListPlaylists(ctx context.Context) ([]gen.Playlist, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	owner := stream.Owner(p)
	ps, err := h.lib().Playlists(ctx, owner)
	if err != nil {
		return nil, err
	}
	out := make([]gen.Playlist, 0, len(ps))
	for _, pl := range ps {
		out = append(out, toPlaylist(pl, owner))
	}
	return out, nil
}

// GetPlaylist implements GET /playlists/{playlistId}.
func (h *Handler) GetPlaylist(ctx context.Context, params gen.GetPlaylistParams) (*gen.PlaylistDetail, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	owner := stream.Owner(p)
	pl, items, err := h.lib().GetPlaylist(ctx, params.PlaylistId, owner)
	if err != nil {
		return nil, err
	}
	return &gen.PlaylistDetail{Playlist: toPlaylist(pl, owner), Items: summaries(items)}, nil
}

// CreatePlaylist implements POST /playlists.
func (h *Handler) CreatePlaylist(ctx context.Context, req *gen.PlaylistInput) (*gen.Playlist, error) {
	return h.savePlaylist(ctx, uuid.Nil, req)
}

// UpdatePlaylist implements PUT /playlists/{playlistId}.
func (h *Handler) UpdatePlaylist(ctx context.Context, req *gen.PlaylistInput, params gen.UpdatePlaylistParams) (*gen.Playlist, error) {
	return h.savePlaylist(ctx, params.PlaylistId, req)
}

func (h *Handler) savePlaylist(ctx context.Context, id uuid.UUID, req *gen.PlaylistInput) (*gen.Playlist, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	owner := stream.Owner(p)
	var items []uuid.UUID
	if req.ItemIds != nil {
		items = append([]uuid.UUID{}, req.ItemIds...)
	} else if id == uuid.Nil {
		items = []uuid.UUID{}
	}
	pl, err := h.lib().SavePlaylist(ctx, id, owner, req.Name, req.Public.Value, items)
	if err != nil {
		return nil, err
	}
	out := toPlaylist(pl, owner)
	return &out, nil
}

// AddToPlaylist implements POST /playlists/{playlistId}/items.
func (h *Handler) AddToPlaylist(ctx context.Context, req *gen.AddToPlaylistReq, params gen.AddToPlaylistParams) (*gen.Playlist, error) {
	if h.lib() == nil {
		return nil, errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return nil, err
	}
	owner := stream.Owner(p)
	pl, err := h.lib().AppendToPlaylist(ctx, params.PlaylistId, owner, req.ItemIds)
	if err != nil {
		return nil, err
	}
	out := toPlaylist(pl, owner)
	return &out, nil
}

// DeletePlaylist implements DELETE /playlists/{playlistId}.
func (h *Handler) DeletePlaylist(ctx context.Context, params gen.DeletePlaylistParams) error {
	if h.lib() == nil {
		return errStarting
	}
	p, err := viewer(ctx)
	if err != nil {
		return err
	}
	return h.lib().DeletePlaylist(ctx, params.PlaylistId, stream.Owner(p))
}
