package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"sort"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/requests"
)

// subtitleEditor allows administrators and users with the "edit subtitles" permission.
func subtitleEditor(ctx context.Context) error {
	p, err := adult(ctx)
	if err != nil {
		return err
	}
	if !p.User.IsAdmin && !p.User.Permissions.EditSubtitles {
		return &StatusError{Status: http.StatusForbidden, Code: "forbidden", Message: "subtitle downloads are not allowed for this account"}
	}
	return nil
}

// subtitleTarget finds the movie or episode in Radarr/Sonarr for Bazarr, and the library to rescan.
func (h *Handler) subtitleTarget(ctx context.Context, itemID uuid.UUID) (requests.SubtitleTarget, uuid.UUID, error) {
	var kind string
	var raw []byte
	var season, episode *int
	var libID uuid.UUID
	// Episodes are looked up through their series' ids.
	err := h.lib().Pool.QueryRow(ctx, `SELECT i.kind, i.library_id, i.season_number, i.episode_number,
			CASE WHEN i.kind = 'episode' THEN COALESCE((SELECT s.provider_ids FROM items se JOIN items s ON s.id = se.parent_id WHERE se.id = i.parent_id),
				(SELECT s.provider_ids FROM items s WHERE s.id = i.parent_id)) ELSE i.provider_ids END
		FROM items i WHERE i.id = $1`, itemID).Scan(&kind, &libID, &season, &episode, &raw)
	if err != nil {
		return requests.SubtitleTarget{}, uuid.Nil, library.ErrNotFound
	}
	ids := map[string]string{}
	_ = json.Unmarshal(raw, &ids)
	s, e := 0, 0
	if season != nil {
		s = *season
	}
	if episode != nil {
		e = *episode
	}
	t, err := h.requestsClient().FindSubtitleTarget(ctx, h.requestSettings(ctx), kind, ids, s, e)
	return t, libID, err
}

func subtitleError(err error) error {
	if errors.Is(err, requests.ErrNotFound) {
		return &StatusError{Status: http.StatusNotFound, Code: "not_in_arr", Message: "Radarr/Sonarr do not manage this title, so Bazarr cannot find it"}
	}
	return upstream(err)
}

// SearchSubtitles implements GET /items/{itemId}/subtitles/search.
func (h *Handler) SearchSubtitles(ctx context.Context, params gen.SearchSubtitlesParams) (*gen.SubtitleSearch, error) {
	if err := subtitleEditor(ctx); err != nil {
		return nil, err
	}
	if h.lib() == nil {
		return nil, errStarting
	}
	s := h.requestSettings(ctx)
	out := &gen.SubtitleSearch{Configured: s.BazarrReady() && (s.RadarrKey != "" || s.SonarrKey != ""), Offers: []gen.SubtitleOffer{}}
	if !out.Configured {
		return out, nil
	}
	t, _, err := h.subtitleTarget(ctx, params.ItemId)
	if err != nil {
		return nil, subtitleError(err)
	}
	offers, err := h.requestsClient().SearchSubtitles(ctx, s, t)
	if err != nil {
		return nil, subtitleError(err)
	}
	sort.SliceStable(offers, func(i, j int) bool { return offers[i].Score > offers[j].Score })
	for _, o := range offers {
		out.Offers = append(out.Offers, gen.SubtitleOffer{Provider: o.Provider, Language: o.Language, Forced: o.Forced, HearingImpaired: o.HearingImpair,
			Score: o.Score, Release: o.Release, Uploader: optString(o.Uploader), Subtitle: o.Subtitle, OriginalFormat: o.OriginalFormat})
	}
	return out, nil
}

// DownloadSubtitle implements POST /items/{itemId}/subtitles/download.
func (h *Handler) DownloadSubtitle(ctx context.Context, req *gen.SubtitleOffer, params gen.DownloadSubtitleParams) error {
	if err := subtitleEditor(ctx); err != nil {
		return err
	}
	if h.lib() == nil {
		return errStarting
	}
	p, _ := principal(ctx)
	t, libID, err := h.subtitleTarget(ctx, params.ItemId)
	if err != nil {
		return subtitleError(err)
	}
	o := requests.SubtitleOffer{Provider: req.Provider, Language: req.Language, Forced: req.Forced, HearingImpair: req.HearingImpaired,
		Subtitle: req.Subtitle, OriginalFormat: req.OriginalFormat}
	if err := h.requestsClient().DownloadSubtitle(ctx, h.requestSettings(ctx), t, o); err != nil {
		return subtitleError(err)
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "subtitle.downloaded", map[string]any{"language": req.Language, "provider": req.Provider})
	// Bazarr saved the file next to the video; a scan makes it selectable.
	if h.lib().Enqueue != nil {
		_ = h.lib().Enqueue(ctx, libID)
	}
	return nil
}

// SearchMissingSubtitles implements POST /items/{itemId}/subtitles/search-missing.
func (h *Handler) SearchMissingSubtitles(ctx context.Context, params gen.SearchMissingSubtitlesParams) error {
	if err := subtitleEditor(ctx); err != nil {
		return err
	}
	if h.lib() == nil {
		return errStarting
	}
	t, _, err := h.subtitleTarget(ctx, params.ItemId)
	if err != nil {
		return subtitleError(err)
	}
	if err := h.requestsClient().SearchMissing(ctx, h.requestSettings(ctx), t); err != nil {
		return subtitleError(err)
	}
	return nil
}
