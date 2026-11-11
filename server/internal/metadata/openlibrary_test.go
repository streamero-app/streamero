package metadata

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenLibraryLookup(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		if r.Header.Get("User-Agent") == "" {
			t.Error("no user agent")
		}
		switch r.URL.Path {
		case "/isbn/9780306406157.json":
			_, _ = w.Write([]byte(`{"title":"X","covers":[42],"works":[{"key":"/works/OL1W"}]}`))
		case "/works/OL1W.json":
			_, _ = w.Write([]byte(`{"description":{"type":"/type/text","value":"From the work."}}`))
		case "/search.json":
			if r.URL.Query().Get("title") != "Unknown Book" || r.URL.Query().Get("author") != "Ana" {
				t.Errorf("search query %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"docs":[{"key":"/works/OL2W","cover_i":7}]}`))
		case "/works/OL2W.json":
			_, _ = w.Write([]byte(`{"description":"Plain text."}`))
		case "/b/id/42-L.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write(make([]byte, 2048))
		case "/b/id/7-L.jpg":
			w.Header().Set("Content-Type", "image/jpeg")
			_, _ = w.Write([]byte{1}) // the 1×1 "no cover" placeholder
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ol := &OpenLibrary{Base: srv.URL, CoversBase: srv.URL}
	ctx := context.Background()
	b, err := ol.Lookup(ctx, "9780306406157", "X", "")
	if err != nil || b.Description != "From the work." || b.CoverID != 42 {
		t.Fatalf("isbn: %+v %v", b, err)
	}
	b, err = ol.Lookup(ctx, "9780000000000", "Unknown Book", "Ana")
	if err != nil || b.Description != "Plain text." || b.CoverID != 7 {
		t.Fatalf("search: %+v %v (%v)", b, err, asked)
	}
	dir := t.TempDir()
	if err := ol.Cover(ctx, 42, filepath.Join(dir, "a.jpg")); err != nil {
		t.Fatal(err)
	}
	if err := ol.Cover(ctx, 7, filepath.Join(dir, "b.jpg")); err == nil {
		t.Fatal("placeholder cover must be refused")
	}
	if _, err := os.Stat(filepath.Join(dir, "b.jpg")); err == nil {
		t.Fatal("placeholder written")
	}
}

func TestOrientationFilter(t *testing.T) {
	if orientationFilter(1) != "" || orientationFilter(6) != "transpose=1," || orientationFilter(8) != "transpose=2," {
		t.Fatal("orientation")
	}
}
