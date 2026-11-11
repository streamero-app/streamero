package library

import (
	"strconv"
	"strings"
	"unicode"
)

// ratingAges maps common certification labels (US film and TV, UK, Australia
// and others) to the minimum viewer age they imply.
var ratingAges = map[string]int{
	"G": 0, "TV-Y": 0, "TV-G": 0, "U": 0, "E": 0, "ALL": 0, "AL": 0, "TP": 0, "L": 0, "ATP": 0, "UC": 0,
	"TV-Y7": 7, "TV-Y7-FV": 7, "PG": 10, "TV-PG": 10,
	"PG-13": 13, "TV-14": 14, "12A": 12, "M": 15, "MA15+": 15, "R": 17, "TV-MA": 17,
	"NC-17": 18, "R18+": 18, "X": 18, "XXX": 18, "R18": 18, "AO": 18,
}

// RatingAge returns the minimum age an official rating implies ("PG-13" → 13,
// "DE-16" / "FSK 16" / "16+" → 16). ok is false for empty or unknown labels.
func RatingAge(rating string) (age int, ok bool) {
	r := strings.ToUpper(strings.TrimSpace(rating))
	if r == "" {
		return 0, false
	}
	if a, found := ratingAges[r]; found {
		return a, true
	}
	// Country prefixes ("US-PG-13", "GB-15") and scheme names ("FSK 12").
	for _, sep := range []string{"-", " ", ":", "/"} {
		if i := strings.Index(r, sep); i > 0 && i <= 4 {
			if a, found := ratingAges[r[i+1:]]; found {
				return a, true
			}
		}
	}
	// Otherwise the first number is the age ("16", "12+", "FSK16", "DE-12").
	digits := strings.FieldsFunc(r, func(c rune) bool { return !unicode.IsDigit(c) })
	if len(digits) > 0 {
		if n, err := strconv.Atoi(digits[0]); err == nil && n <= 21 {
			return n, true
		}
	}
	return 0, false
}

// BlockedRatings returns the ratings among known that exceed maxAge. Unknown
// labels are not blocked: they carry no age to compare.
func BlockedRatings(known []string, maxAge int) []string {
	out := []string{}
	for _, r := range known {
		if a, ok := RatingAge(r); ok && a > maxAge {
			out = append(out, r)
		}
	}
	return out
}
