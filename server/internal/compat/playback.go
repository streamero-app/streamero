package compat

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Stream indexes follow the client convention: the video stream first, then
// the audio tracks, then the subtitle tracks, numbered from 0 per file.
func audioBase(f library.MediaFile) int {
	if hasVideo(f) {
		return 1
	}
	return 0
}

// hasVideo reports whether a file has a picture (music files have none).
func hasVideo(f library.MediaFile) bool { return f.VideoCodec != "" || f.Height != nil }

func subtitleBase(f library.MediaFile) int { return audioBase(f) + len(f.Audio) }

func textSubtitle(codec string) bool {
	switch strings.ToLower(codec) {
	case "subrip", "srt", "ass", "ssa", "webvtt", "vtt", "mov_text", "text":
		return true
	}
	return false
}

func videoRangeType(hdr string) (string, string) {
	switch {
	case hdr == "":
		return "SDR", "SDR"
	case strings.Contains(hdr, "Dolby"):
		return "HDR", "DOVI"
	case hdr == "HLG":
		return "HDR", "HLG"
	}
	return "HDR", "HDR10"
}

func baseStream(typ string, index int) map[string]any {
	return map[string]any{"Type": typ, "Index": index, "IsInterlaced": false, "IsDefault": false, "IsForced": false,
		"IsHearingImpaired": false, "IsOriginal": false, "IsExternal": false, "IsTextSubtitleStream": false,
		"SupportsExternalStream": false, "VideoRange": "Unknown", "VideoRangeType": "Unknown", "AudioSpatialFormat": "None"}
}

// mediaStreams describes a file's streams. defaultAudio is a position in f.Audio (-1 = first).
func mediaStreams(f library.MediaFile, defaultAudio int) []any {
	var out []any
	if hasVideo(f) {
		v := baseStream("Video", 0)
		v["Codec"] = f.VideoCodec
		v["IsDefault"] = true
		v["VideoRange"], v["VideoRangeType"] = videoRangeType(f.HDR)
		if f.Width != nil {
			v["Width"] = *f.Width
		}
		if f.Height != nil {
			v["Height"] = *f.Height
			v["DisplayTitle"] = fmt.Sprintf("%dp %s", *f.Height, strings.ToUpper(f.VideoCodec))
		}
		out = append(out, v)
	}
	if defaultAudio < 0 {
		defaultAudio = 0
	}
	for n, a := range f.Audio {
		m := baseStream("Audio", audioBase(f)+n)
		m["Codec"], m["IsDefault"] = a.Codec, n == defaultAudio
		title := strings.ToUpper(a.Codec)
		if a.Language != "" {
			m["Language"] = a.Language
			title = a.Language + " " + title
		}
		if a.Channels > 0 {
			m["Channels"] = a.Channels
			title += fmt.Sprintf(" %d.%dch", a.Channels-min(a.Channels/6, 1), min(a.Channels/6, 1))
		}
		if a.Title != "" {
			m["Title"] = a.Title
		}
		m["DisplayTitle"] = title
		out = append(out, m)
	}
	for n, sub := range f.Subtitles {
		m := baseStream("Subtitle", subtitleBase(f)+n)
		text := sub.External || textSubtitle(sub.Codec)
		m["Codec"], m["IsForced"], m["IsExternal"] = sub.Codec, sub.Forced, sub.External
		m["IsTextSubtitleStream"], m["SupportsExternalStream"] = text, text
		title := strings.ToUpper(sub.Codec)
		if sub.Language != "" {
			m["Language"] = sub.Language
			title = sub.Language + " " + title
		}
		if sub.Forced {
			title += " (forced)"
		}
		m["DisplayTitle"] = title
		m["DeliveryMethod"] = "Embed"
		if sub.External {
			m["DeliveryMethod"] = "External"
		}
		out = append(out, m)
	}
	if out == nil {
		out = []any{}
	}
	return out
}

// mediaSource describes one version of an item, playable directly by default.
func mediaSource(f library.MediaFile) map[string]any {
	name := f.Label
	if name == "" && f.Height != nil {
		name = fmt.Sprintf("%dp", *f.Height)
	}
	if name == "" {
		name = strings.ToUpper(f.Container)
	}
	src := map[string]any{
		"Id": ID(f.ID), "Protocol": "File", "Type": "Default", "Name": name, "Container": f.Container, "Size": f.Size,
		"IsRemote": false, "ReadAtNativeFramerate": false, "IgnoreDts": false, "IgnoreIndex": false, "GenPtsInput": false,
		"SupportsTranscoding": true, "SupportsDirectStream": true, "SupportsDirectPlay": true, "IsInfiniteStream": false,
		"UseMostCompatibleTranscodingProfile": false, "RequiresOpening": false, "RequiresClosing": false,
		"RequiresLooping": false, "SupportsProbing": true, "TranscodingSubProtocol": "http", "HasSegments": false,
		"VideoType": "VideoFile", "ETag": ID(f.ID)[:16], "MediaStreams": mediaStreams(f, -1), "MediaAttachments": []any{},
		"Formats": []string{}, "RequiredHttpHeaders": map[string]string{},
	}
	if f.DurationMS != nil {
		src["RunTimeTicks"] = ticks(*f.DurationMS)
		if *f.DurationMS > 0 && f.Size > 0 {
			src["Bitrate"] = f.Size * 8 * 1000 / *f.DurationMS
		}
	}
	if len(f.Audio) > 0 {
		src["DefaultAudioStreamIndex"] = audioBase(f)
	}
	return src
}

// --- device profiles ------------------------------------------------------------

// deviceProfile is the part of a client's DeviceProfile used to decide direct play.
type deviceProfile struct {
	MaxStreamingBitrate int `json:"MaxStreamingBitrate"`
	DirectPlayProfiles  []struct {
		Container, AudioCodec, VideoCodec, Type string
	} `json:"DirectPlayProfiles"`
	CodecProfiles []struct {
		Type            string
		Conditions      []profileCondition
		ApplyConditions []profileCondition
	} `json:"CodecProfiles"`
}

type profileCondition struct {
	Condition, Property, Value string
}

// deviceFor turns a client profile into Streamero's device description for file f.
// Without a profile the client is assumed to play the file as it is (clients that
// send none, like many native players, decode everything themselves).
func deviceFor(prof *deviceProfile, f library.MediaFile, allowDirect bool) stream.Device {
	var d stream.Device
	d.HLS = true
	if !allowDirect {
		d.VideoCodecs, d.AudioCodecs = []string{"h264", "hdr"}, []string{"aac", "mp3", "ac3", "eac3"}
		if prof != nil {
			d.MaxBitrate = prof.MaxStreamingBitrate
		}
		return d
	}
	audioCodecs := func() []string {
		var out []string
		for _, a := range f.Audio {
			out = append(out, a.Codec)
		}
		return out
	}
	if prof == nil || len(prof.DirectPlayProfiles) == 0 {
		d.Containers, d.VideoCodecs, d.AudioCodecs = []string{f.Container}, []string{f.VideoCodec, "hdr"}, audioCodecs()
		if prof != nil {
			d.MaxBitrate = prof.MaxStreamingBitrate
		}
		return d
	}
	want := "video"
	if f.VideoCodec == "" {
		want = "audio"
	}
	for _, dp := range prof.DirectPlayProfiles {
		if dp.Type != "" && !strings.EqualFold(dp.Type, want) {
			continue
		}
		if dp.Container == "" {
			d.Containers = append(d.Containers, f.Container)
		}
		d.Containers = append(d.Containers, splitList(strings.ToLower(dp.Container), ",")...)
		if dp.VideoCodec == "" {
			d.VideoCodecs = append(d.VideoCodecs, f.VideoCodec)
		}
		d.VideoCodecs = append(d.VideoCodecs, splitList(strings.ToLower(dp.VideoCodec), ",")...)
		if dp.AudioCodec == "" {
			d.AudioCodecs = append(d.AudioCodecs, audioCodecs()...)
		}
		d.AudioCodecs = append(d.AudioCodecs, splitList(strings.ToLower(dp.AudioCodec), ",")...)
	}
	// Containers named the way clients do ("mp4,m4v", "mkv,webm").
	for _, c := range d.Containers {
		switch c {
		case "m4v", "mov":
			d.Containers = append(d.Containers, "mp4")
		case "matroska", "webm":
			d.Containers = append(d.Containers, "mkv")
		}
	}
	if hdrAllowed(prof, f.HDR) {
		d.VideoCodecs = append(d.VideoCodecs, "hdr")
	}
	d.MaxBitrate = prof.MaxStreamingBitrate
	return d
}

// hdrAllowed reads VideoRangeType conditions of video codec profiles (no condition = allowed).
func hdrAllowed(prof *deviceProfile, hdr string) bool {
	if hdr == "" {
		return true
	}
	_, want := videoRangeType(hdr)
	for _, cp := range prof.CodecProfiles {
		if cp.Type != "" && !strings.EqualFold(cp.Type, "Video") {
			continue
		}
		for _, c := range cp.Conditions {
			if !strings.EqualFold(c.Property, "VideoRangeType") {
				continue
			}
			ok := false
			for _, v := range strings.Split(c.Value, "|") {
				if strings.EqualFold(v, want) || (want == "DOVI" && strings.HasPrefix(strings.ToUpper(v), "DOVI")) {
					ok = true
				}
			}
			if strings.EqualFold(c.Condition, "NotEquals") {
				ok = !ok
			}
			if !ok {
				return false
			}
		}
	}
	return true
}

// --- PlaybackInfo -----------------------------------------------------------------

type playbackBody struct {
	DeviceProfile       *deviceProfile `json:"DeviceProfile"`
	MaxStreamingBitrate *int           `json:"MaxStreamingBitrate"`
	StartTimeTicks      *int64         `json:"StartTimeTicks"`
	AudioStreamIndex    *int           `json:"AudioStreamIndex"`
	SubtitleStreamIndex *int           `json:"SubtitleStreamIndex"`
	MediaSourceID       string         `json:"MediaSourceId"`
	EnableDirectPlay    *bool          `json:"EnableDirectPlay"`
	EnableDirectStream  *bool          `json:"EnableDirectStream"`
}

// fill takes values given as query parameters (GET requests and some clients' POSTs).
func (b *playbackBody) fill(r *http.Request) {
	intp := func(name string) *int {
		if v, err := strconv.Atoi(param(r, name)); err == nil {
			return &v
		}
		return nil
	}
	if b.MaxStreamingBitrate == nil {
		b.MaxStreamingBitrate = intp("MaxStreamingBitrate")
	}
	if b.AudioStreamIndex == nil {
		b.AudioStreamIndex = intp("AudioStreamIndex")
	}
	if b.SubtitleStreamIndex == nil {
		b.SubtitleStreamIndex = intp("SubtitleStreamIndex")
	}
	if b.StartTimeTicks == nil {
		if v, err := strconv.ParseInt(param(r, "StartTimeTicks"), 10, 64); err == nil {
			b.StartTimeTicks = &v
		}
	}
	if b.MediaSourceID == "" {
		b.MediaSourceID = param(r, "MediaSourceId")
	}
	for name, dst := range map[string]**bool{"EnableDirectPlay": &b.EnableDirectPlay, "EnableDirectStream": &b.EnableDirectStream} {
		if *dst == nil {
			if v, ok := boolParam(r, name); ok {
				*dst = &v
			}
		}
	}
}

// pickFile returns the file with id sourceID, or the best version.
func pickFile(files []library.MediaFile, sourceID string) library.MediaFile {
	if id, err := ParseID(sourceID); err == nil {
		for _, f := range files {
			if f.ID == id {
				return f
			}
		}
	}
	return files[0]
}

func newPlaySessionID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return fmt.Sprintf("%x", b)
}

func (s *Server) playbackInfo(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(p) {
		http.NotFound(w, r)
		return
	}
	if s.Stream == nil {
		http.Error(w, "server is starting", http.StatusServiceUnavailable)
		return
	}
	var body playbackBody
	if r.Body != nil {
		_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body) // an empty body is allowed
	}
	body.fill(r)
	files, err := s.filesOf(r.Context(), []uuid.UUID{id})
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	playID := newPlaySessionID()
	if len(files[id]) == 0 {
		writeJSON(w, map[string]any{"MediaSources": []any{}, "PlaySessionId": playID, "ErrorCode": "NoCompatibleStream"})
		return
	}
	f := pickFile(files[id], body.MediaSourceID)
	allowDirect := (body.EnableDirectPlay == nil || *body.EnableDirectPlay) || (body.EnableDirectStream == nil || *body.EnableDirectStream)
	dev := deviceFor(body.DeviceProfile, f, allowDirect)
	if body.MaxStreamingBitrate != nil && *body.MaxStreamingBitrate > 0 {
		dev.MaxBitrate = *body.MaxStreamingBitrate
	}
	if prefs, err := s.Libraries.GetPrefs(r.Context(), ownerOf(p)); err == nil {
		// The profile's limits win over what the device asks for (as in the Streamero API).
		if prefs.MaxBitrateKbps > 0 && (dev.MaxBitrate == 0 || dev.MaxBitrate > prefs.MaxBitrateKbps*1000) {
			dev.MaxBitrate = prefs.MaxBitrateKbps * 1000
		}
		if prefs.MaxHeight > 0 && (dev.MaxHeight == 0 || dev.MaxHeight > prefs.MaxHeight) {
			dev.MaxHeight = prefs.MaxHeight
		}
	}
	req := stream.Request{FileID: &f.ID, Device: dev, StartMS: new(int64)}
	if body.StartTimeTicks != nil && *body.StartTimeTicks > 0 {
		*req.StartMS = *body.StartTimeTicks / 10_000
	}
	if body.AudioStreamIndex != nil {
		if pos := *body.AudioStreamIndex - audioBase(f); pos >= 0 && pos < len(f.Audio) {
			req.AudioIndex = &pos
		}
	}
	info, err := s.Stream.Start(r.Context(), s.Libraries, p, id, req)
	if err != nil {
		code := "NoCompatibleStream"
		switch {
		case errors.Is(err, library.ErrNotFound):
			http.NotFound(w, r)
			return
		case errors.Is(err, stream.ErrTranscodeDenied):
			code = "NotAllowed"
		}
		s.Log.Warn("compat playback failed", "item", id, "err", err)
		writeJSON(w, map[string]any{"MediaSources": []any{}, "PlaySessionId": playID, "ErrorCode": code})
		return
	}
	src := mediaSource(f)
	src["MediaStreams"] = mediaStreams(f, info.AudioIndex)
	if info.AudioIndex >= 0 {
		src["DefaultAudioStreamIndex"] = audioBase(f) + info.AudioIndex
	}
	if info.Method != "direct" {
		src["SupportsDirectPlay"], src["SupportsDirectStream"] = false, false
		src["TranscodingUrl"], src["TranscodingSubProtocol"], src["TranscodingContainer"] = info.URL, "hls", "mp4"
	}
	// Subtitles: sidecar files always by URL; embedded text tracks by URL when the
	// stream is rebuilt (HLS drops them); bitmap tracks only in direct play.
	urls := map[int]string{}
	for _, sub := range info.Subtitles {
		urls[sub.Index] = sub.URL
	}
	for _, ms := range src["MediaStreams"].([]any) {
		m := ms.(map[string]any)
		if m["Type"] != "Subtitle" {
			continue
		}
		pos := m["Index"].(int) - subtitleBase(f)
		url, ok := urls[pos]
		switch {
		case ok && (f.Subtitles[pos].External || info.Method != "direct"):
			m["DeliveryMethod"], m["DeliveryUrl"], m["IsExternalUrl"] = "External", url, false
		case !ok && info.Method != "direct":
			m["DeliveryMethod"] = "Drop"
		}
	}
	src["DefaultSubtitleStreamIndex"] = -1
	if def := info.SubtitleDefault; def >= 0 && def < len(info.Subtitles) {
		src["DefaultSubtitleStreamIndex"] = subtitleBase(f) + info.Subtitles[def].Index
	}
	s.rememberPlay(playID, info.Method, info.Reason)
	writeJSON(w, map[string]any{"MediaSources": []any{src}, "PlaySessionId": playID})
}

// playMemo remembers how a play session is delivered for the dashboard.
type playMemo struct {
	method, reason string
	at             time.Time
}

func (s *Server) rememberPlay(id, method, reason string) {
	s.playsMu.Lock()
	defer s.playsMu.Unlock()
	if s.plays == nil {
		s.plays = map[string]playMemo{}
	}
	now := time.Now()
	for k, v := range s.plays {
		if now.Sub(v.at) > 12*time.Hour {
			delete(s.plays, k)
		}
	}
	s.plays[id] = playMemo{method: method, reason: reason, at: now}
}

func (s *Server) recallPlay(id string) (playMemo, bool) {
	s.playsMu.Lock()
	defer s.playsMu.Unlock()
	m, ok := s.plays[id]
	return m, ok
}

// --- media bytes ------------------------------------------------------------------

// itemFile returns the file of item id named by MediaSourceId (or the best one).
func (s *Server) itemFile(r *http.Request, id uuid.UUID, sourceID string) (library.MediaFile, bool) {
	files, err := s.filesOf(r.Context(), []uuid.UUID{id})
	if err != nil || len(files[id]) == 0 {
		return library.MediaFile{}, false
	}
	if sourceID != "" {
		sid, err := ParseID(sourceID)
		if err != nil {
			return library.MediaFile{}, false
		}
		for _, f := range files[id] {
			if f.ID == sid {
				return f, true
			}
		}
		if sid != id { // some clients send the item id as the source id
			return library.MediaFile{}, false
		}
	}
	return files[id][0], true
}

// media serves /Videos/{itemId}/{file} and /Audio/{itemId}/{file}: the original
// file for "stream[.ext]" and "universal", an HLS session for "master.m3u8".
func (s *Server) media(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(p) || s.Stream == nil {
		http.NotFound(w, r)
		return
	}
	name := chi.URLParam(r, "file")
	f, ok := s.itemFile(r, id, param(r, "MediaSourceId"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	switch {
	case name == "stream" || strings.HasPrefix(name, "stream.") || name == "universal" || name == "download":
		// Direct play: the file as it is, with byte ranges (path checked by ResolveSafePath).
		s.Stream.ServeFile(w, r, f.ID)
	case name == "master.m3u8" || name == "main.m3u8":
		dev := stream.Device{HLS: true, VideoCodecs: splitList(strings.ToLower(param(r, "VideoCodec")), ","),
			AudioCodecs: splitList(strings.ToLower(param(r, "AudioCodec")), ",")}
		if len(dev.VideoCodecs) == 0 {
			dev.VideoCodecs = []string{"h264"}
		}
		if len(dev.AudioCodecs) == 0 {
			dev.AudioCodecs = []string{"aac"}
		}
		if v, err := strconv.Atoi(param(r, "MaxStreamingBitrate")); err == nil {
			dev.MaxBitrate = v
		} else if v, err := strconv.Atoi(param(r, "VideoBitrate")); err == nil {
			dev.MaxBitrate = v
		}
		req := stream.Request{FileID: &f.ID, Device: dev, StartMS: new(int64)}
		if v, err := strconv.ParseInt(param(r, "StartTimeTicks"), 10, 64); err == nil && v > 0 {
			*req.StartMS = v / 10_000
		}
		if v, err := strconv.Atoi(param(r, "AudioStreamIndex")); err == nil {
			if pos := v - audioBase(f); pos >= 0 && pos < len(f.Audio) {
				req.AudioIndex = &pos
			}
		}
		info, err := s.Stream.Start(r.Context(), s.Libraries, p, id, req)
		if err != nil {
			http.Error(w, "playback is not possible", http.StatusConflict)
			return
		}
		// Relative to /…/Videos/{id}/master.m3u8, so it works under any mount prefix.
		w.Header().Set("Location", "../.."+info.URL)
		w.WriteHeader(http.StatusFound)
	default:
		http.NotFound(w, r)
	}
}

// subtitle serves /Videos/{itemId}/{sourceId}/Subtitles/{index}[/{ticks}]/Stream.{format}.
func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(principal(r)) || s.Stream == nil {
		http.NotFound(w, r)
		return
	}
	name := chi.URLParam(r, "file")
	format, ok := strings.CutPrefix(name, "stream.")
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if !ok || err != nil {
		http.NotFound(w, r)
		return
	}
	f, ok := s.itemFile(r, id, chi.URLParam(r, "sourceId"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	pos := index - subtitleBase(f)
	if pos < 0 || pos >= len(f.Subtitles) {
		http.NotFound(w, r)
		return
	}
	s.Stream.ServeSubtitle(w, r, f.ID, pos, format)
}

// bitrateTest sends size random-ish bytes so clients can measure bandwidth.
func bitrateTest(w http.ResponseWriter, r *http.Request) {
	size, err := strconv.Atoi(param(r, "size"))
	if err != nil || size <= 0 {
		size = 100_000
	}
	size = min(size, 10_000_000)
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Content-Length", strconv.Itoa(size))
	buf := make([]byte, 64*1024)
	_, _ = rand.Read(buf)
	for size > 0 {
		n := min(size, len(buf))
		if _, err := w.Write(buf[:n]); err != nil {
			return
		}
		size -= n
	}
}

// --- progress reporting -------------------------------------------------------------

type playReport struct {
	ItemID        string `json:"ItemId"`
	MediaSourceID string `json:"MediaSourceId"`
	PositionTicks *int64 `json:"PositionTicks"`
	PlaySessionID string `json:"PlaySessionId"`
	PlayMethod    string `json:"PlayMethod"`
	Failed        bool   `json:"Failed"`
}

// report reads a playback report from the body, with query parameters and the
// path (legacy /PlayingItems/{itemId} routes) filling the gaps.
func readReport(r *http.Request) playReport {
	var rep playReport
	if r.Body != nil {
		_ = json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&rep)
	}
	if v := chi.URLParam(r, "itemId"); v != "" {
		rep.ItemID = v
	}
	if rep.ItemID == "" {
		rep.ItemID = param(r, "ItemId")
	}
	if rep.PositionTicks == nil {
		if v, err := strconv.ParseInt(param(r, "PositionTicks"), 10, 64); err == nil {
			rep.PositionTicks = &v
		}
	}
	if rep.MediaSourceID == "" {
		rep.MediaSourceID = param(r, "MediaSourceId")
	}
	if rep.PlaySessionID == "" {
		rep.PlaySessionID = param(r, "PlaySessionId")
	}
	if rep.PlayMethod == "" {
		rep.PlayMethod = param(r, "PlayMethod")
	}
	return rep
}

func methodOf(playMethod string) string {
	switch strings.ToLower(playMethod) {
	case "transcode":
		return "transcode"
	case "directstream":
		return "remux"
	}
	return "direct"
}

// playing handles start, progress and stop reports (kind = start|progress|stop).
func (s *Server) playing(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := principal(r)
		rep := readReport(r)
		id, err := ParseID(rep.ItemID)
		if err != nil || !canSee(p) {
			w.WriteHeader(http.StatusNoContent) // nothing to record
			return
		}
		var posMS int64
		if rep.PositionTicks != nil && *rep.PositionTicks > 0 {
			posMS = *rep.PositionTicks / 10_000
		}
		switch kind {
		case "start":
			if s.OnPlayback != nil {
				method, reason := methodOf(rep.PlayMethod), ""
				if m, ok := s.recallPlay(rep.PlaySessionID); ok {
					method, reason = m.method, m.reason
				}
				var dur *int64
				if f, ok := s.itemFile(r, id, rep.MediaSourceID); ok {
					dur = f.DurationMS
				}
				s.OnPlayback(r.Context(), p, id, method, reason, dur, posMS)
			}
		case "progress", "stop":
			if rep.PositionTicks == nil || rep.Failed {
				break
			}
			if s.OnProgress != nil {
				s.OnProgress(p, id, posMS)
			}
			var dur *int64
			if f, ok := s.itemFile(r, id, rep.MediaSourceID); ok {
				dur = f.DurationMS
			}
			if err := s.Libraries.SaveProgress(r.Context(), ownerOf(p), id, posMS, dur, false); err != nil &&
				!errors.Is(err, library.ErrNotFound) {
				s.Log.Warn("compat progress failed", "item", id, "err", err)
			}
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// segments implements /MediaSegments/{itemId} from media analysis (intro, credits, …).
func (s *Server) segments(w http.ResponseWriter, r *http.Request) {
	id, err := ParseID(chi.URLParam(r, "itemId"))
	if err != nil || !canSee(principal(r)) {
		http.NotFound(w, r)
		return
	}
	rows, err := s.Pool.Query(r.Context(), `SELECT g.file_id, g.kind, g.start_ms, g.end_ms FROM media_segments g
		WHERE g.file_id = (SELECT f.id FROM media_files f WHERE f.item_id = $1 ORDER BY f.height DESC NULLS LAST, f.path LIMIT 1)
		ORDER BY g.start_ms`, id)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	defer rows.Close()
	out := []any{}
	for rows.Next() {
		var file uuid.UUID
		var kind string
		var start, end int64
		if err := rows.Scan(&file, &kind, &start, &end); err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		typ := map[string]string{"intro": "Intro", "credits": "Outro", "recap": "Recap", "preview": "Preview"}[kind]
		if typ == "" {
			typ = "Unknown"
		}
		out = append(out, map[string]any{"Id": ID(uuid.NewSHA1(file, []byte(kind))), "ItemId": ID(id), "Type": typ,
			"StartTicks": ticks(start), "EndTicks": ticks(end)})
	}
	writeJSON(w, queryResult(out, len(out), 0))
}
