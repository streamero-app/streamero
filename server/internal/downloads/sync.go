package downloads

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
)

// Progress is a playback state recorded on a device while offline.
type Progress struct {
	ItemID     uuid.UUID
	PositionMS int64
	DurationMS *int64
	Played     bool
	UpdatedAt  time.Time
}

// State is the server's playback state of an item.
type State struct {
	ItemID     uuid.UUID
	PositionMS int64
	Played     bool
	UpdatedAt  time.Time
}

// SyncResult answers a device's sync.
type SyncResult struct {
	Revoked []uuid.UUID
	Items   []State
	Applied int
}

// Sync applies offline progress (newest wins) and reports which of the device's
// items the profile may no longer keep, with the server state of the others.
func (s *Service) Sync(ctx context.Context, p auth.Principal, itemIDs []uuid.UUID, progress []Progress) (SyncResult, error) {
	res := SyncResult{Revoked: []uuid.UUID{}, Items: []State{}}
	owner := Owner(p)
	allowed := map[uuid.UUID]error{}
	check := func(id uuid.UUID) error {
		if err, ok := allowed[id]; ok {
			return err
		}
		err := s.Allowed(ctx, p, id)
		if err != nil && !isDenied(err) {
			return err
		}
		allowed[id] = err
		return err
	}
	for _, pr := range progress {
		err := check(pr.ItemID)
		if err != nil {
			if isDenied(err) {
				continue
			}
			return res, err
		}
		ok, err := s.Library.SaveProgressAt(ctx, owner, pr.ItemID, max(pr.PositionMS, 0), pr.DurationMS, pr.Played, pr.UpdatedAt)
		if err != nil && !errors.Is(err, library.ErrNotFound) {
			return res, err
		}
		if ok {
			res.Applied++
		}
	}
	seen := map[uuid.UUID]bool{}
	var keep []uuid.UUID
	for _, id := range append(append([]uuid.UUID{}, itemIDs...), progressIDs(progress)...) {
		if seen[id] {
			continue
		}
		seen[id] = true
		if err := check(id); err != nil {
			if !isDenied(err) {
				return res, err
			}
			res.Revoked = append(res.Revoked, id)
			continue
		}
		keep = append(keep, id)
	}
	if len(res.Revoked) > 0 {
		// Jobs for revoked items stop as well (their links answer 404 from now on).
		if _, err := s.Pool.Exec(ctx, `DELETE FROM downloads WHERE owner_id = $1 AND item_id = ANY($2)`, owner, res.Revoked); err != nil {
			return res, err
		}
	}
	if len(keep) == 0 {
		return res, nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT item_id, position_ms, played, last_played_at FROM user_item_data
		WHERE owner_id = $1 AND item_id = ANY($2)`, owner, keep)
	if err != nil {
		return res, err
	}
	defer rows.Close()
	for rows.Next() {
		var st State
		if err := rows.Scan(&st.ItemID, &st.PositionMS, &st.Played, &st.UpdatedAt); err != nil {
			return res, err
		}
		res.Items = append(res.Items, st)
	}
	return res, rows.Err()
}

func progressIDs(ps []Progress) []uuid.UUID {
	out := make([]uuid.UUID, len(ps))
	for i, p := range ps {
		out[i] = p.ItemID
	}
	return out
}

// isDenied reports errors that mean "may not keep this offline".
func isDenied(err error) bool {
	return errors.Is(err, library.ErrNotFound) || errors.Is(err, ErrNoPermission) || errors.Is(err, ErrParental)
}
