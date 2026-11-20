package api

import (
	"context"

	"github.com/google/uuid"
	"github.com/ogen-go/ogen/middleware"

	"github.com/streamero-app/streamero/server/internal/access"
	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
)

// AccessGuard is the API middleware that applies library access and parental
// limits to every operation naming an item or a media file in its path or
// query (itemId, fileId, parentId), before the handler runs (D-053). Handlers
// that list items filter their results themselves (see access.Items).
//
// A hidden item answers 404 exactly like a missing one.
func (h *Handler) AccessGuard(req middleware.Request, next middleware.Next) (middleware.Response, error) {
	p, err := principal(req.Context)
	if err != nil || h.lib() == nil {
		return next(req) // signed-out operations and start-up are handled by the operation itself
	}
	for key, v := range req.Params {
		if key.In != "path" && key.In != "query" {
			continue
		}
		var id uuid.UUID
		switch x := v.(type) {
		case uuid.UUID:
			id = x
		case gen.OptUUID:
			if !x.Set {
				continue
			}
			id = x.Value
		default:
			continue
		}
		if err := h.checkAccess(req.Context, p, key.Name, id); err != nil {
			return middleware.Response{}, err
		}
	}
	return next(req)
}

func (h *Handler) checkAccess(ctx context.Context, p auth.Principal, name string, id uuid.UUID) error {
	switch name {
	case "itemId", "parentId":
		return access.Item(ctx, h.lib().Pool, p, id)
	case "fileId":
		return access.File(ctx, h.lib().Pool, p, id)
	}
	return nil
}

// visibleItems drops the items p may not see (library access, parental limit).
func (h *Handler) visibleItems(ctx context.Context, p auth.Principal, items []library.Item) ([]library.Item, error) {
	return access.Items(ctx, h.lib().Pool, p, items)
}

// hiddenLabels returns the ratings hidden from p, for queries that page in SQL.
func (h *Handler) hiddenLabels(ctx context.Context, p auth.Principal) ([]string, error) {
	return access.Labels(ctx, h.lib().Pool, p)
}
