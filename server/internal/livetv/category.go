package livetv

import "strings"

// Normalised categories (LiveTvCategory in the API).
const (
	CatMovies = "movies"
	CatSports = "sports"
	CatNews   = "news"
	CatKids   = "kids"
)

// categoryWords map words found in guide categories and playlist groups (English,
// Croatian and a few common neighbours) to a normalised category. Kids comes
// first: an animated film for children belongs to kids.
var categoryWords = []struct {
	cat   string
	words []string
}{
	{CatKids, []string{"kids", "kid", "children", "child", "childrens", "djeca", "djecji", "dječji", "dečji", "crtani", "crtić",
		"cartoon", "cartoons", "animation", "animated", "kinder", "junior", "baby"}},
	{CatSports, []string{"sport", "sports", "sportski", "football", "soccer", "nogomet", "basketball", "košarka", "kosarka",
		"tennis", "tenis", "hockey", "rugby", "golf", "motorsport", "f1", "nba", "nfl", "nhl", "mlb", "olympics", "athletics", "fussball"}},
	{CatNews, []string{"news", "vijesti", "informativni", "informativno", "info", "current affairs", "dnevnik", "nachrichten", "weather", "politics"}},
	{CatMovies, []string{"movie", "movies", "film", "filmovi", "films", "cinema", "kino", "feature film", "filme"}},
}

// NormalizeCategory maps one category text ("Sports event", "Dječji program",
// "Film / Drama") to a normalised category, or "".
func NormalizeCategory(s string) string {
	t := strings.ToLower(strings.TrimSpace(s))
	if t == "" {
		return ""
	}
	tokens := strings.FieldsFunc(t, func(r rune) bool {
		return r == ' ' || r == '/' || r == ',' || r == '-' || r == '&' || r == '|' || r == ':' || r == ';' || r == '(' || r == ')' || r == '.'
	})
	set := map[string]bool{}
	for _, tok := range tokens {
		set[tok] = true
	}
	for _, c := range categoryWords {
		for _, w := range c.words {
			if strings.Contains(w, " ") {
				if strings.Contains(t, w) {
					return c.cat
				}
			} else if set[w] {
				return c.cat
			}
		}
	}
	return ""
}

// CategoryOf picks the category of a list (the first one that maps).
func CategoryOf(list []string) string {
	for _, c := range list {
		if n := NormalizeCategory(c); n != "" {
			return n
		}
	}
	return ""
}

// validCategory reports whether c is one of the normalised categories.
func validCategory(c string) bool {
	switch c {
	case CatMovies, CatSports, CatNews, CatKids:
		return true
	}
	return false
}
