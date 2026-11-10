package importer

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"
)

// fakeSource is a small stand-in for the source server's public REST API.
type fakeSource struct {
	t         *testing.T
	key       string
	users     []map[string]any
	items     map[string][]map[string]any // user id -> items with user data
	byID      map[string]map[string]any   // any item by id (series)
	playlists map[string][]string         // user id -> visible playlist ids
	lists     map[string]map[string]any   // playlist id -> {Name, OpenAccess, Shares, Items: []id}
	mu        sync.Mutex
	calls     []string
}

func newFakeSource(t *testing.T) *fakeSource {
	return &fakeSource{t: t, key: "secret-key", items: map[string][]map[string]any{}, byID: map[string]map[string]any{},
		playlists: map[string][]string{}, lists: map[string]map[string]any{}}
}

func (f *fakeSource) page(w http.ResponseWriter, r *http.Request, all []map[string]any) {
	start, _ := strconv.Atoi(r.URL.Query().Get("startIndex"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit == 0 {
		limit = len(all)
	}
	end := min(start+limit, len(all))
	if start > end {
		start = end
	}
	_ = json.NewEncoder(w).Encode(map[string]any{"Items": all[start:end], "TotalRecordCount": len(all)})
}

func hasFilter(it map[string]any, filter string) bool {
	ud, _ := it["UserData"].(map[string]any)
	if ud == nil {
		return false
	}
	switch filter {
	case "IsPlayed":
		return ud["Played"] == true
	case "IsFavorite":
		return ud["IsFavorite"] == true
	case "IsResumable":
		switch v := ud["PlaybackPositionTicks"].(type) {
		case int:
			return v > 0
		case int64:
			return v > 0
		}
		return false
	}
	return false
}

func (f *fakeSource) start() *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		f.calls = append(f.calls, r.URL.Path+"?"+r.URL.RawQuery)
		f.mu.Unlock()
		if !strings.Contains(r.Header.Get("Authorization"), `Token="`+f.key+`"`) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		q := r.URL.Query()
		switch {
		case r.URL.Path == "/System/Info":
			_, _ = w.Write([]byte(`{"Id":"srv-1","ServerName":"Old box","Version":"10.10.7"}`))
		case r.URL.Path == "/Users":
			_ = json.NewEncoder(w).Encode(f.users)
		case r.URL.Path == "/Items" && q.Get("ids") != "":
			var out []map[string]any
			for _, id := range strings.Split(q.Get("ids"), ",") {
				if it, ok := f.byID[id]; ok {
					out = append(out, it)
				}
			}
			f.page(w, r, out)
		case r.URL.Path == "/Items" && q.Get("includeItemTypes") == "Playlist":
			var out []map[string]any
			for _, id := range f.playlists[q.Get("userId")] {
				out = append(out, map[string]any{"Id": id, "Name": f.lists[id]["Name"], "Type": "Playlist"})
			}
			f.page(w, r, out)
		case r.URL.Path == "/Items":
			types := strings.Split(q.Get("includeItemTypes"), ",")
			var out []map[string]any
			for _, it := range f.items[q.Get("userId")] {
				for _, typ := range types {
					if it["Type"] == typ && hasFilter(it, q.Get("filters")) {
						out = append(out, it)
					}
				}
			}
			f.page(w, r, out)
		case strings.HasPrefix(r.URL.Path, "/Playlists/") && strings.HasSuffix(r.URL.Path, "/Items"):
			id := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/Playlists/"), "/Items")
			var out []map[string]any
			for _, iid := range f.lists[id]["Items"].([]string) {
				out = append(out, f.byID[iid])
			}
			f.page(w, r, out)
		case strings.HasPrefix(r.URL.Path, "/Playlists/"):
			pl, ok := f.lists[strings.TrimPrefix(r.URL.Path, "/Playlists/")]
			if !ok {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"OpenAccess": pl["OpenAccess"], "Shares": pl["Shares"], "ItemIds": pl["Items"]})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	f.t.Cleanup(srv.Close)
	return srv
}

// item builds a source item; ud may be nil.
func (f *fakeSource) item(user string, it map[string]any) map[string]any {
	if user != "" {
		f.items[user] = append(f.items[user], it)
	}
	f.byID[it["Id"].(string)] = it
	return it
}
