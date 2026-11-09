package metadata

import (
	"strings"
	"testing"
)

func TestParseNFOMovie(t *testing.T) {
	doc := `<?xml version="1.0" encoding="UTF-8" standalone="yes" ?>
<movie>
  <title>Sintel</title>
  <originaltitle>Sintel</originaltitle>
  <plot>A lonely girl searches for her dragon.</plot>
  <tagline>A dragon is born.</tagline>
  <year>2010</year>
  <premiered>2010-09-27</premiered>
  <ratings><rating name="imdb" default="true"><value>7,4</value></rating></ratings>
  <mpaa>PG-13</mpaa>
  <genre>Animation / Fantasy</genre>
  <genre>Short</genre>
  <studio>Blender Institute</studio>
  <director>Colin Levy</director>
  <credits>Esther Wouda</credits>
  <actor><name>Halina Reijn</name><role>Sintel</role></actor>
  <actor><name>Thom Hoffman</name><role>Shaman</role></actor>
  <uniqueid type="tmdb" default="true">45745</uniqueid>
  <uniqueid type="imdb">tt1727587</uniqueid>
  <set><name>Blender Open Movies</name></set>
</movie>
https://www.themoviedb.org/movie/45745`
	n, err := ParseNFO(strings.NewReader(doc))
	if err != nil {
		t.Fatal(err)
	}
	if n.Title != "Sintel" || n.Overview() != "A lonely girl searches for her dragon." || n.Tagline != "A dragon is born." {
		t.Fatalf("text fields: %+v", n)
	}
	if s := n.Score(); s == nil || *s != 7.4 {
		t.Fatalf("score: %v", s)
	}
	if p := n.Premiere(); p == nil || p.Format("2006-01-02") != "2010-09-27" {
		t.Fatalf("premiere: %v", p)
	}
	if g := clean(n.Genres); strings.Join(g, ",") != "Animation,Fantasy,Short" {
		t.Fatalf("genres: %v", g)
	}
	ids := n.ProviderIDs()
	if ids["tmdb"] != "45745" || ids["imdb"] != "tt1727587" {
		t.Fatalf("ids: %v", ids)
	}
	if n.Set.Name != "Blender Open Movies" || len(n.Actors) != 2 || n.Actors[0].Role != "Sintel" || n.Directors[0] != "Colin Levy" {
		t.Fatalf("people/set: %+v", n)
	}
}

func TestParseNFOVariants(t *testing.T) {
	n, err := ParseNFO(strings.NewReader(`<tvshow><title>Caminandes</title><outline>Llamas.</outline><year>2013</year><set>Shorts</set><rating>11</rating></tvshow>`))
	if err != nil {
		t.Fatal(err)
	}
	if n.Overview() != "Llamas." || n.Set.Name != "Shorts" || n.Score() != nil {
		t.Fatalf("variants: %+v", n)
	}
	if p := n.Premiere(); p == nil || p.Year() != 2013 {
		t.Fatalf("year fallback: %v", p)
	}
	if _, err := ParseNFO(strings.NewReader(`https://www.imdb.com/title/tt0133093/`)); err == nil {
		t.Fatal("URL-only nfo should be rejected")
	}
	if _, err := ParseNFO(strings.NewReader(`<musicvideo><title>x</title></musicvideo>`)); err == nil {
		t.Fatal("unsupported document should be rejected")
	}
}
