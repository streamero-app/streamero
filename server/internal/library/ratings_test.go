package library

import (
	"slices"
	"testing"
)

func TestRatingAge(t *testing.T) {
	for in, want := range map[string]int{
		"G": 0, "PG": 10, "PG-13": 13, "R": 17, "NC-17": 18, "TV-MA": 17, "TV-14": 14, "tv-y7": 7,
		"US-PG-13": 13, "GB-15": 15, "DE-12": 12, "FSK 16": 16, "FSK16": 16, "12A": 12, "16+": 16, "15": 15, "U": 0,
	} {
		got, ok := RatingAge(in)
		if !ok || got != want {
			t.Errorf("%q: %d %v, want %d", in, got, ok, want)
		}
	}
	for _, unknown := range []string{"", "Not Rated", "NR", "1999"} {
		if _, ok := RatingAge(unknown); ok {
			t.Errorf("%q has an age", unknown)
		}
	}
	got := BlockedRatings([]string{"PG", "PG-13", "R", "NR", "DE-16", "12"}, 12)
	if !slices.Equal(got, []string{"PG-13", "R", "DE-16"}) {
		t.Fatalf("blocked: %v", got)
	}
}
