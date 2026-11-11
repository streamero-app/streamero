package analyze

import (
	"math/rand"
	"slices"
	"testing"
)

// synthEpisode builds fingerprints: a cold open of coldOpen items, the shared
// intro, unique content; and an end part with the shared credits song followed
// by a short unique tail (a logo card).
func synthEpisode(r *rand.Rand, coldOpen int, intro, credits []uint32) Prints {
	start := append(append(noise(r, coldOpen), intro...), noise(r, 4000-coldOpen-len(intro))...)
	end := append(append(noise(r, 1200), credits...), noise(r, 40)...)
	offset := int64(40 * 60_000)
	return Prints{Intro: start, Credits: end, CreditsOffsetMS: offset, DurationMS: offset + itemMS(len(end))}
}

func near(got, want int64) bool { return got > want-300 && got < want+300 }

func TestMatchSeasonFindsSharedIntroAndCredits(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	intro := noise(r, 320)   // ~40 s
	credits := noise(r, 600) // ~74 s
	colds := []int{100, 400, 0, 250, 700}
	var eps []Prints
	for _, c := range colds {
		eps = append(eps, synthEpisode(r, c, intro, credits))
	}
	// Episode 3 ends with a different song: its credits are not shared.
	eps[3] = synthEpisode(r, colds[3], intro, noise(r, 600))
	got := MatchSeason(eps)
	for i, c := range colds {
		in := got[i].Intro
		if in == nil || !near(in.StartMS, itemMS(c+edgeTrim)) || !near(in.EndMS, itemMS(c+len(intro)-edgeTrim)) {
			t.Errorf("episode %d intro %+v, want ~%d..%d", i, in, itemMS(c+edgeTrim), itemMS(c+len(intro)-edgeTrim))
		}
		cr := got[i].Credits
		if i == 3 {
			if cr != nil {
				t.Errorf("episode 3 has unique credits, got %+v", cr)
			}
			continue
		}
		// The credits run to the end of the file (the logo card is included).
		if cr == nil || !near(cr.StartMS, eps[i].CreditsOffsetMS+itemMS(1200+edgeTrim)) || cr.EndMS != eps[i].DurationMS {
			t.Errorf("episode %d credits %+v", i, cr)
		}
	}
}

func TestMatchSeasonSingleEpisodeAndNoPrints(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	if got := MatchSeason([]Prints{synthEpisode(r, 10, noise(r, 300), noise(r, 300))}); got[0].Intro != nil || got[0].Credits != nil {
		t.Fatalf("single episode: %+v", got)
	}
	if got := MatchSeason([]Prints{{}, {}}); got[0].Intro != nil || got[1].Credits != nil {
		t.Fatalf("empty: %+v", got)
	}
}

func TestConsensusOutvotesOddPeer(t *testing.T) {
	c := consensus([]Span{{10_000, 50_000}, {11_000, 51_000}, {200_000, 260_000}, {10_500, 49_000}})
	if c == nil || c.StartMS != 10_500 || c.EndMS != 50_000 {
		t.Fatalf("consensus %+v", c)
	}
	if consensus(nil) != nil {
		t.Fatal("no candidates")
	}
}

func TestPeers(t *testing.T) {
	if p := peers(0, 10); !slices.Equal(p, []int{1, 2, 3, 4}) {
		t.Fatal(p)
	}
	if p := peers(5, 10); !slices.Equal(p, []int{4, 6, 3, 7}) {
		t.Fatal(p)
	}
	if p := peers(1, 2); !slices.Equal(p, []int{0}) {
		t.Fatal(p)
	}
}

func TestBlackFrameCredits(t *testing.T) {
	log := `[blackdetect @ 0x1] black_start:0.5 black_end:2 black_duration:1.5
[blackdetect @ 0x1] black_start:150.2 black_end:180.04 black_duration:29.84
[blackdetect @ 0x1] black_start:181.5 black_end:290 black_duration:108.5`
	spans := ParseBlackDetect(log, 1_000_000)
	if len(spans) != 3 || spans[1].StartMS != 1_150_200 || spans[1].EndMS != 1_180_040 {
		t.Fatalf("parsed %+v", spans)
	}
	// Short gaps are bridged; the run ends 5 s before the end (logo card).
	c, ok := CreditsFromBlack(spans, 1_295_000)
	if !ok || c.StartMS != 1_150_200 || c.EndMS != 1_295_000 {
		t.Fatalf("credits %+v %v", c, ok)
	}
	// A fade to black in the middle of the window is not credits.
	if _, ok := CreditsFromBlack(spans[:2], 1_295_000); ok {
		t.Fatal("black run far from the end accepted")
	}
	// Too short.
	if _, ok := CreditsFromBlack([]Span{{100_000, 108_000}}, 110_000); ok {
		t.Fatal("8 s black accepted")
	}
	if _, ok := CreditsFromBlack(nil, 1000); ok {
		t.Fatal("nothing accepted")
	}
}

func TestFingerprintEncoding(t *testing.T) {
	fp := []uint32{0, 1, 0xFFFFFFFF, 123456789}
	if got := decodePrint(encodePrint(fp)); !slices.Equal(got, fp) {
		t.Fatal(got)
	}
	if encodePrint(nil) != nil {
		t.Fatal("empty print stored")
	}
	if got := parseFpcalc("DURATION=12\nFINGERPRINT=1,4294967295,-1,7\n"); !slices.Equal(got, []uint32{1, 0xFFFFFFFF, 0xFFFFFFFF, 7}) {
		t.Fatal(got)
	}
	if parseFpcalc("ERROR: x") != nil {
		t.Fatal("no fingerprint line")
	}
}
