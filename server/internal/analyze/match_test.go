package analyze

import (
	"math/rand"
	"testing"
)

func noise(r *rand.Rand, n int) []uint32 {
	out := make([]uint32, n)
	for i := range out {
		out[i] = r.Uint32()
	}
	return out
}

func TestLongestFindsSharedIntroAtDifferentOffsets(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	intro := noise(r, 300) // ~37 s of theme song
	// Episode A: 20 s cold open, intro, rest. Episode B: intro after 50 s.
	a := append(append(noise(r, 160), intro...), noise(r, 2000)...)
	b := append(append(noise(r, 400), intro...), noise(r, 2000)...)
	// A little noise inside the shared part still matches.
	for i := 170; i < 400; i += 37 {
		a[i] ^= 0b101
	}
	m, ok := Longest(a, b, 15, 150)
	if !ok {
		t.Fatal("no match")
	}
	if m.AStart != 160+edgeTrim || m.AEnd != 460-edgeTrim || m.BStart != 400+edgeTrim || m.BEnd != 700-edgeTrim {
		t.Fatalf("match %+v", m)
	}
	if s := m.Seconds(); s < 33 || s > 36 {
		t.Fatalf("seconds %.1f", s)
	}
}

func TestLongestRejectsShortOrMissingPassages(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	shared := noise(r, 40) // ~5 s: a sting, not an intro
	a := append(append(noise(r, 100), shared...), noise(r, 500)...)
	b := append(append(noise(r, 300), shared...), noise(r, 500)...)
	if m, ok := Longest(a, b, 15, 150); ok {
		t.Fatalf("short passage accepted: %+v", m)
	}
	if _, ok := Longest(noise(r, 1000), noise(r, 1000), 15, 150); ok {
		t.Fatal("unrelated audio matched")
	}
}

func TestChapterSegments(t *testing.T) {
	for title, want := range map[string]string{
		"Opening": "intro", "OP": "intro", "Intro": "intro", "End Credits": "credits", "ED": "credits",
		"Previously on": "recap", "Preview": "preview", "Chapter 3": "", "Edward's speech": "", "Opportunity": "",
	} {
		if got := ChapterKind(title); got != want {
			t.Errorf("%q: %q, want %q", title, got, want)
		}
	}
	chs := []Chapter{{0, "Prologue"}, {90_000, "Opening"}, {180_000, "Part A"}, {1_300_000, "Ending"}, {1_390_000, "Preview"}, {1_400_000, "x"}}
	segs := SegmentsFromChapters(chs, 1_420_000)
	if len(segs) != 3 || segs[0] != (Segment{"intro", 90_000, 180_000, "chapter"}) ||
		segs[1] != (Segment{"credits", 1_300_000, 1_390_000, "chapter"}) || segs[2].Kind != "preview" {
		t.Fatalf("segments %+v", segs)
	}
}

func TestLongestIgnoresSteadySound(t *testing.T) {
	hum := make([]uint32, 400) // 50 s of the same fingerprint value: a hum, not a theme song
	for i := range hum {
		hum[i] = 0xdeadbeef
	}
	if m, ok := Longest(hum, hum, 15, 150); ok {
		t.Fatalf("steady sound matched: %+v", m)
	}
}
