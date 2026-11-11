package dlna

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// newRequest builds the browse context of an HTTP request.
func (s *Server) newRequest(r *http.Request, v *Viewer) request {
	return request{v: v, prof: Detect(r.Header), base: s.baseFor(r)}
}

func (s *Server) controlCDS(w http.ResponseWriter, r *http.Request) {
	a, err := ParseAction(r)
	if err != nil {
		writeFault(w, &soapError{Code: errInvalidAction, Desc: "Invalid Action"})
		return
	}
	ctx := r.Context()
	_, v := s.Current(ctx)
	if v == nil {
		http.NotFound(w, r)
		return
	}
	switch a.Name {
	case "GetSearchCapabilities":
		writeSOAP(w, typeCDS, a.Name, []pair{{"SearchCaps", "dc:title,upnp:class"}})
	case "GetSortCapabilities":
		writeSOAP(w, typeCDS, a.Name, []pair{{"SortCaps", "dc:title,dc:date"}})
	case "GetSystemUpdateID":
		writeSOAP(w, typeCDS, a.Name, []pair{{"Id", strconv.FormatUint(uint64(s.systemUpdateID(ctx)), 10)}})
	case "Browse":
		s.browseAction(w, r, a, v)
	case "Search":
		s.searchAction(w, r, a, v)
	case "X_GetFeatureList":
		writeSOAP(w, typeCDS, a.Name, []pair{{"FeatureList", featureList}})
	case "X_SetBookmark":
		s.setBookmark(w, r, a, v)
	default:
		writeFault(w, &soapError{Code: errInvalidAction, Desc: "Invalid Action"})
	}
}

// featureList answers Samsung's X_GetFeatureList: browsing starts at the root for every media type.
const featureList = `<?xml version="1.0" encoding="UTF-8"?><Features xmlns="urn:schemas-upnp-org:av:avs" ` +
	`xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:schemaLocation="urn:schemas-upnp-org:av:avs http://www.upnp.org/schemas/av/avs.xsd">` +
	`<Feature name="samsung.com_BASICVIEW" version="1"><container id="0" type="object.item.audioItem"/>` +
	`<container id="0" type="object.item.videoItem"/><container id="0" type="object.item.imageItem"/></Feature></Features>`

func uintArg(a Action, name string) (int, bool) {
	v := strings.TrimSpace(a.Args[name])
	if v == "" {
		return 0, true
	}
	n, err := strconv.ParseUint(v, 10, 32)
	if err != nil {
		return 0, false
	}
	return int(min(n, 1<<20)), true
}

func (s *Server) browseAction(w http.ResponseWriter, r *http.Request, a Action, v *Viewer) {
	start, ok1 := uintArg(a, "StartingIndex")
	count, ok2 := uintArg(a, "RequestedCount")
	flag := a.Args["BrowseFlag"]
	if !ok1 || !ok2 || (flag != "BrowseMetadata" && flag != "BrowseDirectChildren") {
		writeFault(w, &soapError{Code: errInvalidArgs, Desc: "Invalid Args"})
		return
	}
	objs, total, err := s.Browse(r.Context(), s.newRequest(r, v), a.Args["ObjectID"], flag == "BrowseDirectChildren", start, count, a.Args["SortCriteria"])
	if err != nil {
		s.fault(w, err)
		return
	}
	s.writeResult(r.Context(), w, "Browse", objs, total)
}

func (s *Server) fault(w http.ResponseWriter, err error) {
	var se *soapError
	if errors.As(err, &se) {
		writeFault(w, se)
		return
	}
	s.Log.Warn("dlna request failed", "err", err)
	writeFault(w, &soapError{Code: errActionFailed, Desc: "Action Failed"})
}

func (s *Server) writeResult(ctx context.Context, w http.ResponseWriter, action string, objs []Object, total int) {
	writeSOAP(w, typeCDS, action, []pair{{"Result", DIDL(objs)}, {"NumberReturned", strconv.Itoa(len(objs))},
		{"TotalMatches", strconv.Itoa(total)}, {"UpdateID", strconv.FormatUint(uint64(s.systemUpdateID(ctx)), 10)}})
}

var (
	containsRE = regexp.MustCompile(`(?i)(?:dc:title|upnp:artist|upnp:album|dc:creator)\s+contains\s+"((?:[^"\\]|\\.)*)"`)
	classRE    = regexp.MustCompile(`(?i)upnp:class\s+(?:derivedfrom|=)\s+"([^"]*)"`)
)

// searchAction handles the cheap case of Search: a title contains "…" term,
// optionally limited by class (video or audio). Other criteria find nothing.
func (s *Server) searchAction(w http.ResponseWriter, r *http.Request, a Action, v *Viewer) {
	start, ok1 := uintArg(a, "StartingIndex")
	count, ok2 := uintArg(a, "RequestedCount")
	if !ok1 || !ok2 {
		writeFault(w, &soapError{Code: errInvalidArgs, Desc: "Invalid Args"})
		return
	}
	objs, total, err := s.Search(r.Context(), s.newRequest(r, v), a.Args["SearchCriteria"], start, count)
	if err != nil {
		s.fault(w, err)
		return
	}
	s.writeResult(r.Context(), w, "Search", objs, total)
}

// Search finds playable items whose title matches the criteria's "contains" term.
func (s *Server) Search(ctx context.Context, q request, criteria string, start, count int) ([]Object, int, error) {
	m := containsRE.FindStringSubmatch(criteria)
	if m == nil || !q.v.CanSeeLibraries() {
		return nil, 0, nil
	}
	term := strings.ReplaceAll(m[1], `\"`, `"`)
	class := ""
	if c := classRE.FindStringSubmatch(criteria); c != nil {
		class = c[1]
	}
	found, err := s.Library.Search(ctx, q.v.Owner, term, 200)
	if err != nil {
		return nil, 0, err
	}
	found, err = s.visible(ctx, q.v, found)
	if err != nil {
		return nil, 0, err
	}
	kept := found[:0]
	for _, it := range found {
		video := it.Kind == "movie" || it.Kind == "episode"
		audio := it.Kind == "track"
		switch {
		case strings.HasPrefix(class, "object.item.videoItem") && !video,
			strings.HasPrefix(class, "object.item.audioItem") && !audio,
			strings.HasPrefix(class, "object.item.imageItem"),
			strings.HasPrefix(class, "object.item") && !video && !audio:
			continue
		case strings.HasPrefix(class, "object.container"):
			continue
		}
		kept = append(kept, it)
	}
	if err := s.Library.AttachSeries(ctx, kept); err != nil {
		return nil, 0, err
	}
	objs, err := s.objects(ctx, q, page(kept, start, count), "i:", "0", false)
	return objs, len(kept), err
}

// setBookmark stores the position a Samsung TV reports when playback stops.
func (s *Server) setBookmark(w http.ResponseWriter, r *http.Request, a Action, v *Viewer) {
	n, ok := parseID(a.Args["ObjectID"])
	pos, okPos := uintArg(a, "PosSecond")
	if !ok || !okPos || (n.kind != "item" && n.kind != "cwitem") {
		writeFault(w, &soapError{Code: errInvalidArgs, Desc: "Invalid Args"})
		return
	}
	q := s.newRequest(r, v)
	if _, _, _, err := s.item(r.Context(), q, n.item); err != nil {
		s.fault(w, err)
		return
	}
	posMS := int64(pos) * 1000
	if n.kind == "cwitem" {
		// The resume stream starts at the saved position: the TV counts from there.
		var prev int64
		_ = s.Pool.QueryRow(r.Context(), `SELECT position_ms FROM user_item_data WHERE owner_id = $1 AND item_id = $2`, v.Owner, n.item).Scan(&prev)
		posMS += prev
	}
	if err := s.Library.SaveProgress(r.Context(), v.Owner, n.item, posMS, nil, false); err != nil {
		s.fault(w, err)
		return
	}
	s.tracker.forget(clientIP(r), n.item)
	writeSOAP(w, typeCDS, a.Name, nil)
}

// systemUpdateID changes whenever the library does (last item change, in seconds).
func (s *Server) systemUpdateID(ctx context.Context) uint32 {
	var t *time.Time
	_ = s.Pool.QueryRow(ctx, `SELECT max(updated_at) FROM items`).Scan(&t)
	if t == nil {
		return 1
	}
	return uint32(t.Unix() & 0x7fffffff) // #nosec G115 -- masked to 31 bits
}

func (s *Server) controlCMS(w http.ResponseWriter, r *http.Request) {
	a, err := ParseAction(r)
	if err != nil {
		writeFault(w, &soapError{Code: errInvalidAction, Desc: "Invalid Action"})
		return
	}
	switch a.Name {
	case "GetProtocolInfo":
		writeSOAP(w, typeCMS, a.Name, []pair{{"Source", sourceProtocols()}, {"Sink", ""}})
	case "GetCurrentConnectionIDs":
		writeSOAP(w, typeCMS, a.Name, []pair{{"ConnectionIDs", "0"}})
	case "GetCurrentConnectionInfo":
		if strings.TrimSpace(a.Args["ConnectionID"]) != "0" {
			writeFault(w, &soapError{Code: 706, Desc: "Invalid connection reference"})
			return
		}
		writeSOAP(w, typeCMS, a.Name, []pair{{"RcsID", "-1"}, {"AVTransportID", "-1"}, {"ProtocolInfo", ""},
			{"PeerConnectionManager", ""}, {"PeerConnectionID", "-1"}, {"Direction", "Output"}, {"Status", "OK"}})
	default:
		writeFault(w, &soapError{Code: errInvalidAction, Desc: "Invalid Action"})
	}
}

// clientIP is the TV's address (no proxies: the DLNA port is LAN only).
func clientIP(r *http.Request) string {
	host := r.RemoteAddr
	if i := strings.LastIndex(host, ":"); i > 0 {
		host = host[:i]
	}
	return strings.Trim(host, "[]")
}
