package analyze

import (
	"regexp"
	"slices"
	"strconv"
)

// Search windows and plausible lengths of the passages a season shares.
const (
	IntroMinSec   = 15
	IntroMaxSec   = 150
	CreditsMinSec = 15
	CreditsMaxSec = 300
	// maxPeers is how many other episodes of the season each episode is compared with.
	maxPeers = 4
	// creditsTailMS: credits that stop this close to the end run to the end (a logo card follows).
	creditsTailMS = 10_000
)

// Span is a passage of a file in milliseconds.
type Span struct{ StartMS, EndMS int64 }

func (s Span) len() int64 { return s.EndMS - s.StartMS }

// Prints are an episode's audio fingerprints of its start and its end.
type Prints struct {
	Intro           []uint32
	Credits         []uint32
	CreditsOffsetMS int64 // where the Credits fingerprint starts in the file
	DurationMS      int64
}

// SeasonSpans is what the season comparison found for one episode.
type SeasonSpans struct {
	Intro, Credits *Span
}

// peers lists the episodes closest to i in season order (i-1, i+1, i-2, …).
func peers(i, n int) []int {
	var out []int
	for d := 1; len(out) < maxPeers && (i-d >= 0 || i+d < n); d++ {
		if i-d >= 0 {
			out = append(out, i-d)
		}
		if i+d < n && len(out) < maxPeers {
			out = append(out, i+d)
		}
	}
	return out
}

// MatchSeason finds the intro and the credits each episode shares with the other
// episodes of its season. Every episode is compared with up to four neighbours and
// keeps the passage most of them agree on, so one odd episode (a recap, a
// different end song) does not decide for the others.
func MatchSeason(eps []Prints) []SeasonSpans {
	out := make([]SeasonSpans, len(eps))
	for i, e := range eps {
		var intros, credits []Span
		for _, j := range peers(i, len(eps)) {
			if m, ok := Longest(e.Intro, eps[j].Intro, IntroMinSec, IntroMaxSec); ok {
				intros = append(intros, Span{itemMS(m.AStart), itemMS(m.AEnd)})
			}
			if m, ok := Longest(e.Credits, eps[j].Credits, CreditsMinSec, CreditsMaxSec); ok {
				credits = append(credits, Span{e.CreditsOffsetMS + itemMS(m.AStart), e.CreditsOffsetMS + itemMS(m.AEnd)})
			}
		}
		out[i].Intro = consensus(intros)
		if c := consensus(credits); c != nil {
			if e.DurationMS > 0 && e.DurationMS-c.EndMS < creditsTailMS {
				c.EndMS = e.DurationMS
			}
			out[i].Credits = c
		}
	}
	return out
}

func itemMS(items int) int64 { return secMS(float64(items) * ItemSeconds) }

// overlaps reports whether a and b share at least half of the shorter one.
func overlaps(a, b Span) bool {
	shared := min(a.EndMS, b.EndMS) - max(a.StartMS, b.StartMS)
	return shared > 0 && shared*2 >= min(a.len(), b.len())
}

// consensus picks the passage most candidates agree on and returns the median of
// those candidates' starts and ends; nil without candidates.
func consensus(cands []Span) *Span {
	if len(cands) == 0 {
		return nil
	}
	best, bestN := 0, -1
	for i, c := range cands {
		n := 0
		for _, o := range cands {
			if overlaps(c, o) {
				n++
			}
		}
		if n > bestN || (n == bestN && c.len() > cands[best].len()) {
			best, bestN = i, n
		}
	}
	var starts, ends []int64
	for _, o := range cands {
		if overlaps(cands[best], o) {
			starts, ends = append(starts, o.StartMS), append(ends, o.EndMS)
		}
	}
	slices.Sort(starts)
	slices.Sort(ends)
	s := Span{starts[len(starts)/2], ends[len(ends)/2]}
	if s.EndMS <= s.StartMS {
		s = cands[best]
	}
	return &s
}

var blackLine = regexp.MustCompile(`black_start:\s*([0-9.]+)\s+black_end:\s*([0-9.]+)`)

// ParseBlackDetect reads ffmpeg blackdetect log lines; times are shifted by offsetMS.
func ParseBlackDetect(log string, offsetMS int64) []Span {
	var out []Span
	for _, m := range blackLine.FindAllStringSubmatch(log, -1) {
		s, err1 := strconv.ParseFloat(m[1], 64)
		e, err2 := strconv.ParseFloat(m[2], 64)
		if err1 != nil || err2 != nil || e <= s {
			continue
		}
		out = append(out, Span{offsetMS + secMS(s), offsetMS + secMS(e)})
	}
	return out
}

// CreditsFromBlack finds end credits on a dark background: the last mostly-black
// run (gaps up to 3 s, e.g. a bright title card, are bridged) that lasts at least
// CreditsMinSec and reaches the end of the file (up to a short logo card).
func CreditsFromBlack(black []Span, durationMS int64) (Span, bool) {
	if len(black) == 0 || durationMS <= 0 {
		return Span{}, false
	}
	const gap = 3_000
	black = slices.Clone(black)
	slices.SortFunc(black, func(a, b Span) int { return int(a.StartMS - b.StartMS) })
	runs := []Span{black[0]}
	for _, b := range black[1:] {
		last := &runs[len(runs)-1]
		if b.StartMS-last.EndMS <= gap {
			last.EndMS = max(last.EndMS, b.EndMS)
		} else {
			runs = append(runs, b)
		}
	}
	r := runs[len(runs)-1]
	if durationMS-r.EndMS > creditsTailMS || r.len() < CreditsMinSec*1000 || r.len() > CreditsMaxSec*1000 {
		return Span{}, false
	}
	return Span{r.StartMS, durationMS}, true
}
