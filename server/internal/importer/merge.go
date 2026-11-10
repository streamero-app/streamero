package importer

import "time"

// State is the playback state of one viewer for one item.
type State struct {
	PositionMS   int64
	Played       bool
	PlayCount    int
	LastPlayedAt time.Time
	Favorite     bool
}

// Incoming is the state read from the source server for one item.
type Incoming struct {
	PositionMS int64
	Played     bool
	PlayCount  int
	LastPlayed *time.Time // nil when the source does not know
	Favorite   bool
}

// FromUserData converts the source's user data (positions in 100 ns ticks).
func FromUserData(d UserData) Incoming {
	in := Incoming{PositionMS: TicksToMS(d.PlaybackPositionTicks), Played: d.Played, PlayCount: d.PlayCount, Favorite: d.IsFavorite}
	if d.LastPlayedDate != nil && d.LastPlayedDate.Year() > 1 {
		// Postgres keeps microseconds; truncating makes a second import compare equal.
		t := d.LastPlayedDate.UTC().Truncate(time.Microsecond)
		in.LastPlayed = &t
	}
	if in.Played {
		in.PositionMS = 0
		in.PlayCount = max(in.PlayCount, 1)
	}
	return in
}

// Combine merges two source items that map to the same Streamero item.
func Combine(a, b Incoming) Incoming {
	out := a
	if b.LastPlayed != nil && (a.LastPlayed == nil || b.LastPlayed.After(*a.LastPlayed)) {
		out.PositionMS, out.Played, out.LastPlayed = b.PositionMS, b.Played, b.LastPlayed
	}
	out.PlayCount = max(a.PlayCount, b.PlayCount)
	out.Favorite = a.Favorite || b.Favorite
	return out
}

// Merge combines the existing Streamero state (nil = none) with the imported one.
// The more recent progress wins (by last played date); play counts take the larger
// value and a favourite is never removed, so importing twice changes nothing.
// keptNewer reports that Streamero's own, newer progress was kept.
func Merge(cur *State, in Incoming, now time.Time) (next State, changed, keptNewer bool) {
	if cur == nil {
		next = State{PositionMS: in.PositionMS, Played: in.Played, PlayCount: in.PlayCount, Favorite: in.Favorite, LastPlayedAt: now}
		if in.LastPlayed != nil {
			next.LastPlayedAt = *in.LastPlayed
		}
		return next, true, false
	}
	next = *cur
	next.Favorite = cur.Favorite || in.Favorite
	next.PlayCount = max(cur.PlayCount, in.PlayCount)
	untouched := !cur.Played && cur.PositionMS == 0 && cur.PlayCount == 0
	newer := in.LastPlayed != nil && in.LastPlayed.After(cur.LastPlayedAt)
	hasProgress := in.Played || in.PositionMS > 0
	switch {
	case newer || (untouched && hasProgress):
		next.Played, next.PositionMS = in.Played, in.PositionMS
		if in.LastPlayed != nil {
			next.LastPlayedAt = *in.LastPlayed
		}
	case hasProgress && (in.Played != cur.Played || in.PositionMS != cur.PositionMS):
		keptNewer = true
	}
	return next, next != *cur, keptNewer
}
