package livetv

import (
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Padding limits (also in the API description).
const (
	maxPrePadding  = 3600
	maxPostPadding = 7200
)

// Window is when a recording occupies a tuner: the programme with its padding.
// Negative padding counts as none and padding is capped.
func Window(start, end time.Time, pre, post int) (time.Time, time.Time) {
	pre = min(max(pre, 0), maxPrePadding)
	post = min(max(post, 0), maxPostPadding)
	return start.Add(-time.Duration(pre) * time.Second), end.Add(time.Duration(post) * time.Second)
}

// Slot is a recording competing for the tuners of one source.
type Slot struct {
	ID         uuid.UUID
	Tuner      uuid.UUID
	Start, End time.Time // padded window
	Priority   int
	Created    time.Time
	Running    bool // already recording: always kept
}

// Resolution says which recordings get a tuner. ConflictsWith lists, for every
// recording left out, the accepted ones it overlaps on the same tuner source.
type Resolution struct {
	Accepted      map[uuid.UUID]bool
	ConflictsWith map[uuid.UUID][]uuid.UUID
}

// ResolveConflicts assigns tuners: running recordings first, then by priority
// (higher first), then the earlier-created one. A recording is accepted when,
// throughout its window, the accepted recordings of its source plus itself
// stay within the source's capacity (0 = unlimited). Live viewers are not
// counted: a recording takes a tuner from a viewer when it starts.
func ResolveConflicts(slots []Slot, capacity func(tuner uuid.UUID) int) Resolution {
	order := append([]Slot(nil), slots...)
	sort.SliceStable(order, func(i, j int) bool {
		a, b := order[i], order[j]
		if a.Running != b.Running {
			return a.Running
		}
		if a.Priority != b.Priority {
			return a.Priority > b.Priority
		}
		if !a.Created.Equal(b.Created) {
			return a.Created.Before(b.Created)
		}
		if !a.Start.Equal(b.Start) {
			return a.Start.Before(b.Start)
		}
		return a.ID.String() < b.ID.String()
	})
	res := Resolution{Accepted: map[uuid.UUID]bool{}, ConflictsWith: map[uuid.UUID][]uuid.UUID{}}
	byTuner := map[uuid.UUID][]Slot{}
	for _, s := range order {
		limit := capacity(s.Tuner)
		taken := byTuner[s.Tuner]
		if s.Running || limit <= 0 || peakOverlap(taken, s.Start, s.End)+1 <= limit {
			res.Accepted[s.ID] = true
			byTuner[s.Tuner] = append(taken, s)
			continue
		}
		for _, t := range taken {
			if t.Start.Before(s.End) && s.Start.Before(t.End) {
				res.ConflictsWith[s.ID] = append(res.ConflictsWith[s.ID], t.ID)
			}
		}
	}
	return res
}

// peakOverlap is the largest number of slots running at once inside [from, to).
func peakOverlap(slots []Slot, from, to time.Time) int {
	type ev struct {
		at    time.Time
		delta int
	}
	var evs []ev
	for _, s := range slots {
		if !s.Start.Before(to) || !from.Before(s.End) {
			continue
		}
		st := s.Start
		if st.Before(from) {
			st = from
		}
		evs = append(evs, ev{st, 1}, ev{s.End, -1})
	}
	// Ends before starts at the same instant: back-to-back windows do not overlap.
	sort.Slice(evs, func(i, j int) bool {
		if evs[i].at.Equal(evs[j].at) {
			return evs[i].delta < evs[j].delta
		}
		return evs[i].at.Before(evs[j].at)
	})
	cur, peak := 0, 0
	for _, e := range evs {
		cur += e.delta
		peak = max(peak, cur)
	}
	return peak
}

// RuleSpec is what a series rule matches.
type RuleSpec struct {
	Title     string
	ChannelID *uuid.UUID // nil = any channel
	NewOnly   bool
}

// Airing is a programme on a channel, as series rules see it.
type Airing struct {
	ProgramID  uuid.UUID
	ChannelID  uuid.UUID
	Title      string
	Start      time.Time
	IsNew      bool
	IsRepeat   bool
	EpisodeKey string
}

// sameTitle compares programme titles loosely (case, accents, punctuation).
func sameTitle(a, b string) bool {
	na, nb := normName(a), normName(b)
	if na == "" || nb == "" {
		return strings.EqualFold(strings.TrimSpace(a), strings.TrimSpace(b))
	}
	return na == nb
}

// Matches reports whether an airing belongs to the rule. With NewOnly, an
// airing the guide marks as previously shown (and not as new) is skipped.
func (r RuleSpec) Matches(a Airing) bool {
	if !sameTitle(r.Title, a.Title) {
		return false
	}
	if r.ChannelID != nil && *r.ChannelID != a.ChannelID {
		return false
	}
	if r.NewOnly && a.IsRepeat && !a.IsNew {
		return false
	}
	return true
}

// SelectAirings picks the airings a rule records. Every programme is recorded
// once (a guide channel mapped to several channels shows it more than once;
// the earliest-listed channel wins). With NewOnly, each episode is recorded
// once, at its first airing, and episodes in have (already recorded or
// scheduled) are skipped; airings without an episode key are kept.
func SelectAirings(r RuleSpec, airings []Airing, have map[string]bool) []Airing {
	sorted := append([]Airing(nil), airings...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].Start.Before(sorted[j].Start) })
	var out []Airing
	seenProg := map[uuid.UUID]bool{}
	seenKey := map[string]bool{}
	for _, a := range sorted {
		if !r.Matches(a) || seenProg[a.ProgramID] {
			continue
		}
		if r.NewOnly && a.EpisodeKey != "" {
			if have[a.EpisodeKey] || seenKey[a.EpisodeKey] {
				continue
			}
			seenKey[a.EpisodeKey] = true
		}
		seenProg[a.ProgramID] = true
		out = append(out, a)
	}
	return out
}
