// Package analyze finds chapters, skippable segments (intro, credits, recap,
// preview) and trickplay thumbnails for media files.
package analyze

import (
	"math/bits"
	"regexp"
	"sort"
	"strings"
)

// ItemSeconds is the duration one Chromaprint fingerprint value covers
// (4096-sample frames at 11025 Hz with 2/3 overlap).
const ItemSeconds = 4096.0 / 3.0 / 11025.0

// Match is a shared passage between two fingerprints, in items.
type Match struct {
	AStart, AEnd int // [AStart, AEnd) in a
	BStart, BEnd int // the same passage in b
}

// Seconds returns the passage length.
func (m Match) Seconds() float64 { return float64(m.AEnd-m.AStart) * ItemSeconds }

const (
	maxBitDiff = 6  // values differing in up to 6 of 32 bits still match
	maxGap     = 10 // tolerate ~1.2 s of noise inside a passage
	minVotes   = 5
	edgeTrim   = 12 // ~1.5 s
)

// varied reports whether a passage changes over time like music or speech.
// Steady sounds (hum, noise, silence) give near-constant fingerprints that
// would otherwise "match" any other steady sound.
func varied(fp []uint32) bool {
	// Neighbouring values overlap in time and always look alike; compare ~1 s apart.
	const lag = 8
	if len(fp) <= lag {
		return false
	}
	changes := 0
	for i := lag; i < len(fp); i++ {
		if bits.OnesCount32(fp[i]^fp[i-lag]) > maxBitDiff {
			changes++
		}
	}
	return changes*100 >= (len(fp)-lag)*30
}

// Longest finds the longest passage a and b share (e.g. the intro song of two
// episodes), within [minSec, maxSec]. ok is false when there is none.
func Longest(a, b []uint32, minSec, maxSec float64) (Match, bool) {
	if len(a) == 0 || len(b) == 0 {
		return Match{}, false
	}
	// Candidate alignments: score every offset by how many values nearly agree.
	// Offsets of different files rarely line up to the same Chromaprint frame
	// grid, so exact equality is too strict; ~O(len(a)·len(b)/stride) popcounts.
	type cand struct{ shift, n int }
	var cands []cand
	for shift := -(len(a) - 1); shift < len(b); shift++ {
		n := 0
		for i := max(0, -shift); i < len(a) && i+shift < len(b); i += 2 {
			if bits.OnesCount32(a[i]^b[i+shift]) <= maxBitDiff {
				n++
			}
		}
		if n >= minVotes {
			cands = append(cands, cand{shift, n})
		}
	}
	sort.Slice(cands, func(i, j int) bool { return cands[i].n > cands[j].n })
	if len(cands) > 8 {
		cands = cands[:8]
	}
	var best Match
	found := false
	for _, c := range cands {
		start, last, hits := -1, -1, 0
		consider := func(s, e, hits int) {
			// A shared passage agrees almost everywhere; chance agreement is patchy.
			if hits*100 < (e-s)*70 {
				return
			}
			// Fingerprints blur over ~2 s windows: trim the edges so a skip never cuts into the episode.
			s, e = s+edgeTrim, e-edgeTrim
			if e <= s || !varied(a[s:e]) {
				return
			}
			m := Match{AStart: s, AEnd: e, BStart: s + c.shift, BEnd: e + c.shift}
			if sec := m.Seconds(); sec >= minSec && sec <= maxSec && (!found || m.AEnd-m.AStart > best.AEnd-best.AStart) {
				best, found = m, true
			}
		}
		for i := max(0, -c.shift); i < len(a) && i+c.shift < len(b); i++ {
			if bits.OnesCount32(a[i]^b[i+c.shift]) <= maxBitDiff {
				if start < 0 || i-last > maxGap {
					if start >= 0 {
						consider(start, last+1, hits)
					}
					start, hits = i, 0
				}
				last = i
				hits++
			}
		}
		if start >= 0 {
			consider(start, last+1, hits)
		}
	}
	return best, found
}

var chapterKinds = []struct {
	kind string
	re   *regexp.Regexp
}{
	{"recap", regexp.MustCompile(`\b(recap|previously)\b`)},
	{"intro", regexp.MustCompile(`\b(intro|opening|op|opening credits|main titles?|title sequence)\b`)},
	{"credits", regexp.MustCompile(`\b(credits|end credits|ending|ed|outro|closing)\b`)},
	{"preview", regexp.MustCompile(`\b(preview|next time|next episode|sneak peek)\b`)},
}

// ChapterKind classifies a chapter title ("Opening", "End Credits"…); "" when ordinary.
func ChapterKind(title string) string {
	t := strings.ToLower(strings.TrimSpace(title))
	for _, c := range chapterKinds {
		if c.re.MatchString(t) {
			return c.kind
		}
	}
	return ""
}

// Chapter is a named position in a file.
type Chapter struct {
	StartMS int64  `json:"startMs"`
	Title   string `json:"title"`
}

// Segment is a skippable part of a file.
type Segment struct {
	Kind           string
	StartMS, EndMS int64
	Source         string
}

// SegmentsFromChapters turns intro/credits/recap/preview chapters into segments.
func SegmentsFromChapters(chs []Chapter, durationMS int64) []Segment {
	var out []Segment
	seen := map[string]bool{}
	for i, c := range chs {
		kind := ChapterKind(c.Title)
		if kind == "" || seen[kind] {
			continue
		}
		end := durationMS
		if i+1 < len(chs) {
			end = chs[i+1].StartMS
		}
		if end-c.StartMS < 5_000 {
			continue // a marker, not a passage
		}
		seen[kind] = true
		out = append(out, Segment{Kind: kind, StartMS: c.StartMS, EndMS: end, Source: "chapter"})
	}
	return out
}
