package compat

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/livetv"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Live TV for media clients (D-041), from the public API description: channel
// and guide listing (/LiveTv/Channels, /LiveTv/Programs, /LiveTv/GuideInfo,
// /LiveTv/Info), channel items and their playback through PlaybackInfo (an HLS
// transcoding URL with the live window). Recordings and timers are answered
// empty: clients manage them in Streamero's own apps.

func (s *Server) liveRoutes(r chi.Router) {
	r.Get("/livetv/info", s.liveInfo)
	r.Get("/livetv/guideinfo", s.liveGuideInfo)
	r.Get("/livetv/channels", s.liveChannels)
	r.Get("/livetv/channels/{channelId}", s.liveChannel)
	r.Get("/livetv/programs", s.livePrograms)
	r.Post("/livetv/programs", s.livePrograms)
	r.Get("/livetv/programs/recommended", s.liveRecommended)
	r.Get("/livetv/programs/{programId}", s.liveProgram)
	for _, p := range []string{"/livetv/recordings", "/livetv/timers", "/livetv/seriestimers", "/livetv/recordings/series", "/livetv/recordings/groups"} {
		r.Get(p, emptyResult)
	}
	r.Get("/livetv/recordings/folders", emptyResult)
}

// canWatchLive: the liveTv permission, never from a kids profile (channels and
// recordings have no age ratings to filter by).
func canWatchLive(p auth.Principal) bool {
	return !p.IsKid() && (p.User.IsAdmin || p.User.Permissions.LiveTV)
}

// liveReady returns the service when the caller may watch Live TV.
func (s *Server) liveReady(w http.ResponseWriter, r *http.Request) (*livetv.Service, auth.Principal, bool) {
	p := principal(r)
	if s.LiveTV == nil || !canWatchLive(p) {
		writeJSON(w, queryResult(nil, 0, 0))
		return nil, p, false
	}
	return s.LiveTV, p, true
}

func ticksTime(t time.Time) string { return t.UTC().Format("2006-01-02T15:04:05.0000000Z") }

func imageTag(v string) string {
	sum := sha256.Sum256([]byte(v))
	return hex.EncodeToString(sum[:8])
}

func (s *Server) channelDTO(c livetv.Channel, withProgram bool) map[string]any {
	d := map[string]any{"Name": c.Name, "Id": ID(c.ID), "ServerId": ID(s.ServerID), "Type": "TvChannel", "ChannelType": "TV",
		"Number": c.Number, "ChannelNumber": c.Number, "MediaType": "Video", "IsFolder": false, "LocationType": "Remote",
		"ImageTags": map[string]string{}, "BackdropImageTags": []string{},
		"UserData": map[string]any{"IsFavorite": c.Favorite, "Played": false, "PlaybackPositionTicks": 0, "PlayCount": 0, "Key": ID(c.ID)}}
	if c.LogoURL != "" {
		d["ImageTags"] = map[string]string{"Primary": imageTag(c.ID.String() + c.LogoURL[:strings.Index(c.LogoURL+"?", "?")])}
	}
	if withProgram && c.Now != nil {
		d["CurrentProgram"] = s.programDTO(*c.Now, c)
	}
	return d
}

func (s *Server) programDTO(p livetv.Program, c livetv.Channel) map[string]any {
	d := map[string]any{"Name": p.Title, "Id": ID(p.ID), "ServerId": ID(s.ServerID), "Type": "Program", "MediaType": "Video",
		"IsFolder": false, "ChannelId": ID(c.ID), "ChannelName": c.Name, "ChannelNumber": c.Number,
		"StartDate": ticksTime(p.Start), "EndDate": ticksTime(p.End), "RunTimeTicks": ticks(p.End.Sub(p.Start).Milliseconds()),
		"Overview": p.Description, "Genres": orEmpty(p.Categories), "IsMovie": p.Category == livetv.CatMovies,
		"IsSports": p.Category == livetv.CatSports, "IsNews": p.Category == livetv.CatNews, "IsKids": p.Category == livetv.CatKids,
		"IsSeries": p.Season != nil || p.Episode != nil || p.SubTitle != "", "IsLive": p.IsLive, "IsPremiere": p.IsPremiere,
		"IsRepeat": p.IsRepeat, "ImageTags": map[string]string{}, "BackdropImageTags": []string{}, "LocationType": "Remote"}
	if p.SubTitle != "" {
		d["EpisodeTitle"] = p.SubTitle
	}
	if p.Season != nil {
		d["ParentIndexNumber"] = *p.Season
	}
	if p.Episode != nil {
		d["IndexNumber"] = *p.Episode
	}
	if p.Year != nil {
		d["ProductionYear"] = *p.Year
	}
	if p.RecordingID != nil {
		d["TimerId"] = ID(*p.RecordingID)
	}
	if p.SeriesRuleID != nil {
		d["SeriesTimerId"] = ID(*p.SeriesRuleID)
	}
	return d
}

func (s *Server) liveInfo(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	enabled := s.LiveTV != nil && canWatchLive(p)
	services := []any{}
	if enabled {
		services = append(services, map[string]any{"Name": "Streamero", "HomePageUrl": "", "Status": "Ok", "StatusMessage": "",
			"Version": "", "HasUpdateAvailable": false, "IsVisible": true, "Tuners": []string{}})
	}
	writeJSON(w, map[string]any{"Services": services, "IsEnabled": enabled, "EnabledUsers": []string{}})
}

func (s *Server) liveGuideInfo(w http.ResponseWriter, _ *http.Request) {
	now := time.Now().UTC()
	writeJSON(w, map[string]any{"StartDate": ticksTime(now.Add(-time.Hour)), "EndDate": ticksTime(now.Add(14 * 24 * time.Hour))})
}

func paging(r *http.Request, n int) (int, int) {
	start, _ := strconv.Atoi(param(r, "StartIndex"))
	start = min(max(start, 0), n)
	end := n
	if l, err := strconv.Atoi(param(r, "Limit")); err == nil && l >= 0 && start+l < n {
		end = start + l
	}
	return start, end
}

func (s *Server) liveChannels(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := s.liveReady(w, r)
	if !ok {
		return
	}
	fav, _ := boolParam(r, "IsFavorite")
	list, err := svc.Channels(r.Context(), ownerOf(p), livetv.ChannelFilter{FavoritesOnly: fav})
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	withProgram, set := boolParam(r, "AddCurrentProgram")
	if !set {
		withProgram = true
	}
	start, end := paging(r, len(list))
	items := make([]any, 0, end-start)
	for _, c := range list[start:end] {
		items = append(items, s.channelDTO(c, withProgram))
	}
	writeJSON(w, queryResult(items, len(list), start))
}

func (s *Server) liveChannel(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "channelId"))
	if err != nil || !s.writeLiveItem(w, r, id) {
		http.NotFound(w, r)
	}
}

// writeLiveItem answers GET /Items/{id} for a channel or programme (false: not one).
func (s *Server) writeLiveItem(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	p := principal(r)
	if s.LiveTV == nil || !canWatchLive(p) {
		return false
	}
	if c, err := s.LiveTV.ChannelByID(r.Context(), ownerOf(p), id); err == nil {
		writeJSON(w, s.channelDTO(c, true))
		return true
	}
	if pr, c, err := s.LiveTV.ProgramOn(r.Context(), ownerOf(p), id, nil); err == nil {
		writeJSON(w, s.programDTO(pr, c))
		return true
	}
	return false
}

func (s *Server) liveProgram(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "programId"))
	if err != nil || !s.writeLiveItem(w, r, id) {
		http.NotFound(w, r)
	}
}

// programQuery is the programme filter, from the query string or a POST body.
type programQuery struct {
	ChannelIDs                                           []string
	MinStart, MaxStart, MinEnd, MaxEnd                   *time.Time
	HasAired, IsAiring, IsMovie, IsSports, IsNews, IsKids *bool
}

func parseClientTime(v string) *time.Time {
	if v == "" {
		return nil
	}
	for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05.0000000Z", "2006-01-02T15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return &t
		}
	}
	return nil
}

func readProgramQuery(r *http.Request) programQuery {
	vals := map[string]string{}
	for k, v := range r.URL.Query() {
		if len(v) > 0 {
			vals[strings.ToLower(k)] = v[0]
		}
	}
	var ids []string
	if r.Method == http.MethodPost && r.Body != nil {
		var body map[string]any
		if json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&body) == nil {
			for k, v := range body {
				switch x := v.(type) {
				case string:
					vals[strings.ToLower(k)] = x
				case bool:
					vals[strings.ToLower(k)] = strconv.FormatBool(x)
				case []any:
					if strings.EqualFold(k, "ChannelIds") {
						for _, e := range x {
							if s, ok := e.(string); ok {
								ids = append(ids, s)
							}
						}
					}
				}
			}
		}
	}
	if v := vals["channelids"]; v != "" {
		ids = append(ids, splitList(v, ",")...)
	}
	b := func(k string) *bool {
		if v, err := strconv.ParseBool(vals[k]); err == nil {
			return &v
		}
		return nil
	}
	return programQuery{ChannelIDs: ids, MinStart: parseClientTime(vals["minstartdate"]), MaxStart: parseClientTime(vals["maxstartdate"]),
		MinEnd: parseClientTime(vals["minenddate"]), MaxEnd: parseClientTime(vals["maxenddate"]), HasAired: b("hasaired"),
		IsAiring: b("isairing"), IsMovie: b("ismovie"), IsSports: b("issports"), IsNews: b("isnews"), IsKids: b("iskids")}
}

type liveProgramRow struct {
	p livetv.Program
	c livetv.Channel
}

func (s *Server) queryPrograms(r *http.Request, svc *livetv.Service, p auth.Principal, q programQuery) ([]liveProgramRow, error) {
	now := time.Now().UTC()
	from := now.Add(-time.Hour)
	var to time.Time
	if q.MinEnd != nil {
		from = *q.MinEnd
	} else if q.MinStart != nil {
		from = *q.MinStart
	}
	to = from.Add(24 * time.Hour)
	if q.MaxStart != nil {
		to = q.MaxStart.Add(time.Second)
	} else if q.MaxEnd != nil {
		to = *q.MaxEnd
	}
	if to.Sub(from) > 15*24*time.Hour {
		to = from.Add(15 * 24 * time.Hour)
	}
	rows, err := svc.Guide(r.Context(), ownerOf(p), from, to, livetv.ChannelFilter{})
	if err != nil {
		return nil, err
	}
	want := map[uuid.UUID]bool{}
	for _, raw := range q.ChannelIDs {
		if id, err := ParseID(raw); err == nil {
			want[id] = true
		}
	}
	is := func(f *bool, v bool) bool { return f == nil || *f == v }
	var out []liveProgramRow
	for _, row := range rows {
		if len(want) > 0 && !want[row.Channel.ID] {
			continue
		}
		for _, pr := range row.Programs {
			switch {
			case q.MinStart != nil && pr.Start.Before(*q.MinStart), q.MaxStart != nil && pr.Start.After(*q.MaxStart),
				q.MinEnd != nil && pr.End.Before(*q.MinEnd), q.MaxEnd != nil && pr.End.After(*q.MaxEnd):
				continue
			case !is(q.HasAired, !pr.End.After(now)), !is(q.IsAiring, !pr.Start.After(now) && pr.End.After(now)),
				!is(q.IsMovie, pr.Category == livetv.CatMovies), !is(q.IsSports, pr.Category == livetv.CatSports),
				!is(q.IsNews, pr.Category == livetv.CatNews), !is(q.IsKids, pr.Category == livetv.CatKids):
				continue
			}
			out = append(out, liveProgramRow{pr, row.Channel})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].p.Start.Before(out[j].p.Start) })
	return out, nil
}

func (s *Server) livePrograms(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := s.liveReady(w, r)
	if !ok {
		return
	}
	rows, err := s.queryPrograms(r, svc, p, readProgramQuery(r))
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	start, end := paging(r, len(rows))
	items := make([]any, 0, end-start)
	for _, x := range rows[start:end] {
		items = append(items, s.programDTO(x.p, x.c))
	}
	writeJSON(w, queryResult(items, len(rows), start))
}

// liveRecommended lists what is airing now (clients show it as "On now").
func (s *Server) liveRecommended(w http.ResponseWriter, r *http.Request) {
	svc, p, ok := s.liveReady(w, r)
	if !ok {
		return
	}
	q := readProgramQuery(r)
	if q.IsAiring == nil && q.HasAired == nil {
		t := true
		q.IsAiring = &t
	}
	rows, err := s.queryPrograms(r, svc, p, q)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	start, end := paging(r, len(rows))
	items := make([]any, 0, end-start)
	for _, x := range rows[start:end] {
		items = append(items, s.programDTO(x.p, x.c))
	}
	writeJSON(w, queryResult(items, len(rows), start))
}

// livePlaybackInfo answers PlaybackInfo for a channel (false: not a channel).
func (s *Server) livePlaybackInfo(w http.ResponseWriter, r *http.Request, id uuid.UUID) bool {
	p := principal(r)
	if s.LiveTV == nil || !canWatchLive(p) {
		return false
	}
	ch, err := s.LiveTV.ChannelByID(r.Context(), ownerOf(p), id)
	if err != nil {
		return false
	}
	var body playbackBody
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	}
	body.fill(r)
	dev := liveDevice(body.DeviceProfile)
	if body.MaxStreamingBitrate != nil && *body.MaxStreamingBitrate > 0 {
		dev.MaxBitrate = *body.MaxStreamingBitrate
	}
	playID := newPlaySessionID()
	pb, err := s.LiveTV.Play(r.Context(), p, id, dev)
	if err != nil {
		code := "NoCompatibleStream"
		if errors.Is(err, livetv.ErrTunersBusy) {
			code = "RateLimitExceeded"
		}
		s.Log.Warn("compat live playback failed", "channel", ch.Name, "err", err)
		writeJSON(w, map[string]any{"MediaSources": []any{}, "PlaySessionId": playID, "ErrorCode": code})
		return true
	}
	streams := []any{}
	if ch.VideoCodec != "" {
		v := baseStream("Video", 0)
		v["Codec"] = ch.VideoCodec
		if ch.Height != nil {
			v["Height"] = *ch.Height
		}
		streams = append(streams, v)
	}
	if ch.AudioCodec != "" {
		a := baseStream("Audio", len(streams))
		a["Codec"], a["Channels"], a["IsDefault"] = ch.AudioCodec, 2, true
		streams = append(streams, a)
	}
	src := map[string]any{"Id": ID(ch.ID), "Name": ch.Name, "Path": "", "Protocol": "Http", "Type": "Default", "Container": "ts",
		"IsRemote": false, "IsInfiniteStream": true, "RequiresOpening": false, "RequiresClosing": false, "SupportsProbing": false,
		"SupportsDirectPlay": false, "SupportsDirectStream": false, "SupportsTranscoding": true, "ReadAtNativeFramerate": false,
		"TranscodingUrl": pb.URL, "TranscodingSubProtocol": "hls", "TranscodingContainer": "mp4", "MediaStreams": streams,
		"DefaultSubtitleStreamIndex": -1}
	if ch.AudioCodec != "" {
		src["DefaultAudioStreamIndex"] = len(streams) - 1
	}
	s.rememberPlay(playID, pb.Method, pb.Reason)
	writeJSON(w, map[string]any{"MediaSources": []any{src}, "PlaySessionId": playID})
	return true
}

// liveDevice is what a client plays over HLS: H.264 with AAC/MP3/AC3 (what every
// client of this API handles there) plus the codecs of its video transcoding profiles.
func liveDevice(prof *deviceProfile) stream.Device {
	d := stream.Device{HLS: true, Containers: []string{"ts", "mpegts"}, VideoCodecs: []string{"h264"},
		AudioCodecs: []string{"aac", "mp3", "ac3", "eac3"}}
	if prof == nil {
		return d
	}
	d.MaxBitrate = prof.MaxStreamingBitrate
	for _, tp := range prof.TranscodingProfiles {
		if strings.EqualFold(tp.Type, "Video") {
			d.VideoCodecs = append(d.VideoCodecs, splitList(strings.ToLower(tp.VideoCodec), ",")...)
			d.AudioCodecs = append(d.AudioCodecs, splitList(strings.ToLower(tp.AudioCodec), ",")...)
		}
	}
	return d
}

// liveImage redirects a channel's primary image to its signed logo (false: not a channel).
func (s *Server) liveImage(w http.ResponseWriter, r *http.Request, raw, typ string) bool {
	if s.LiveTV == nil || typ != "primary" {
		return false
	}
	id, err := ParseID(raw)
	if err != nil {
		return false
	}
	// Images are public in this API (players load them without headers); a logo is not private.
	c, err := s.LiveTV.ChannelByID(r.Context(), uuid.Nil, id)
	if err != nil || c.LogoURL == "" {
		return false
	}
	http.Redirect(w, r, c.LogoURL, http.StatusFound)
	return true
}
