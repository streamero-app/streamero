package stream

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
)

// Errors for the API layer.
var (
	ErrNotPlayable       = errors.New("this item has no playable file")
	ErrTranscodeDenied   = errors.New("this file needs transcoding, which your account may not use")
	ErrTranscoderFailure = errors.New("the transcoder could not start")
)

// URLTTL is how long stream links stay valid (a long film plus pauses).
const URLTTL = 8 * time.Hour

// Request is a playback request.
type Request struct {
	FileID     *uuid.UUID
	AudioIndex *int
	StartMS    *int64
	Device     Device
	// SubtitleIndex is the viewer's subtitle choice (position in the file's
	// subtitle list, -1 = none, nil = automatic: a forced bitmap track the
	// subtitle preference picks is burned in).
	SubtitleIndex *int
	// BurnSubtitle burns SubtitleIndex in even when it is a text track.
	BurnSubtitle bool
}

// BitmapSubtitle is a picture-based subtitle track, offered for burn-in.
type BitmapSubtitle struct {
	Index    int
	Codec    string
	Language string
	Forced   bool
}

// Subtitle is a playable subtitle track.
type Subtitle struct {
	Index    int
	Language string
	Forced   bool
	External bool
	URL      string
}

// Info tells the player what to load.
type Info struct {
	FileID     uuid.UUID
	Method     string
	URL        string
	StartMS    int64
	DurationMS *int64
	Audio      []library.AudioStream
	AudioIndex int
	Subtitles  []Subtitle
	Reason     string
	// SubtitleDefault is the position in Subtitles to show (-1 = off).
	SubtitleDefault int
	Chapters        []Chapter
	Segments        []Segment
	Trickplay       *Trickplay
	// BitmapSubtitles can be chosen with Request.SubtitleIndex (burned in).
	BitmapSubtitles []BitmapSubtitle
	// BurnedSubtitle is the subtitle track drawn into this stream (-1 = none).
	BurnedSubtitle int
}

// Chapter is a named position (from media analysis).
type Chapter struct {
	StartMS int64  `json:"startMs"`
	Title   string `json:"title"`
}

// Segment is a skippable part (intro, credits, recap, preview).
type Segment struct {
	Kind           string
	StartMS, EndMS int64
}

// Trickplay describes seek-bar thumbnail sheets.
type Trickplay struct {
	IntervalMS int `json:"intervalMs"`
	Width      int `json:"width"`
	Height     int `json:"height"`
	Columns    int `json:"columns"`
	Rows       int `json:"rows"`
	Count      int `json:"count"`
	Sheets     int `json:"sheets"`
}

// analysis loads chapters, segments and trickplay of a file (empty when not analyzed yet).
func (s *Server) analysis(ctx context.Context, fileID uuid.UUID) ([]Chapter, []Segment, *Trickplay) {
	var chRaw, tpRaw []byte
	_ = s.Pool.QueryRow(ctx, `SELECT chapters, trickplay FROM media_files WHERE id = $1`, fileID).Scan(&chRaw, &tpRaw)
	var chs []Chapter
	_ = json.Unmarshal(chRaw, &chs)
	var tp *Trickplay
	if len(tpRaw) > 0 {
		tp = &Trickplay{}
		if json.Unmarshal(tpRaw, tp) != nil {
			tp = nil
		}
	}
	var segs []Segment
	rows, err := s.Pool.Query(ctx, `SELECT kind, start_ms, end_ms FROM media_segments WHERE file_id = $1 ORDER BY start_ms`, fileID)
	if err == nil {
		for rows.Next() {
			var sg Segment
			if rows.Scan(&sg.Kind, &sg.StartMS, &sg.EndMS) == nil {
				segs = append(segs, sg)
			}
		}
		rows.Close()
	}
	return chs, segs, tp
}

// Analysis returns the chapters and trickplay of a file for the item page.
func (s *Server) Analysis(ctx context.Context, fileID uuid.UUID) ([]Chapter, *Trickplay) {
	chs, _, tp := s.analysis(ctx, fileID)
	return chs, tp
}

// Owner returns the id that playback state belongs to (active profile or user).
func Owner(p auth.Principal) uuid.UUID {
	if p.Profile != nil {
		return p.Profile.ID
	}
	return p.User.ID
}

// LoadKey returns the stream signing key, creating it on first use.
func LoadKey(ctx context.Context, st *settings.Store) ([]byte, error) {
	var enc string
	ok, err := st.Get(ctx, "stream.key", &enc)
	if err != nil {
		return nil, err
	}
	if ok {
		return base64.StdEncoding.DecodeString(enc)
	}
	key := make([]byte, 32)
	if _, err := rand.Read(key); err != nil {
		return nil, err
	}
	if err := st.Set(ctx, "stream.key", base64.StdEncoding.EncodeToString(key)); err != nil {
		return nil, err
	}
	return key, nil
}

func loadSubtitles(ctx context.Context, db interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}, fileID uuid.UUID) ([]library.SubtitleStream, error) {
	var raw []byte
	if err := db.QueryRow(ctx, `SELECT subtitles FROM media_files WHERE id = $1`, fileID).Scan(&raw); err != nil {
		return nil, err
	}
	var subs []library.SubtitleStream
	return subs, json.Unmarshal(raw, &subs)
}

// Start prepares playback of an item for a principal.
func (s *Server) Start(ctx context.Context, lib *library.Service, p auth.Principal, itemID uuid.UUID, req Request) (Info, error) {
	_, files, _, err := lib.GetItem(ctx, itemID, Owner(p))
	if err != nil {
		return Info{}, err
	}
	if len(files) == 0 {
		return Info{}, ErrNotPlayable
	}
	if req.FileID != nil && !slices.ContainsFunc(files, func(x library.MediaFile) bool { return x.ID == *req.FileID }) {
		return Info{}, library.ErrNotFound
	}
	f, err := pickFile(files, req.FileID, func(path string) bool {
		_, err := library.ResolveSafePath(s.Roots, path)
		return err == nil
	})
	if err != nil {
		return Info{}, err
	}
	prefs, _ := lib.GetPrefs(ctx, Owner(p))
	audio := library.PickAudio(f.Audio, prefs.AudioLanguage)
	if req.AudioIndex != nil && *req.AudioIndex >= 0 && *req.AudioIndex < len(f.Audio) {
		audio = *req.AudioIndex
	}
	if len(f.Audio) == 0 {
		audio = -1
	}
	start := int64(0)
	if req.StartMS != nil {
		start = max(0, *req.StartMS)
	} else {
		_ = s.Pool.QueryRow(ctx, `SELECT position_ms FROM user_item_data WHERE owner_id = $1 AND item_id = $2 AND NOT played`,
			Owner(p), itemID).Scan(&start)
	}

	sub := s.subtitleChoice(f, audio, prefs, req.SubtitleIndex)
	plan := DecideWithSubtitle(f, audio, sub, req.BurnSubtitle && req.SubtitleIndex != nil, req.Device)
	if b := plan.Burn; b != nil && b.External {
		if p, err := sidecarPath(s.Roots, f.Subtitles[b.Index]); err == nil {
			b.Path = p
		} else {
			plan = Decide(f, audio, req.Device) // the sidecar is gone or outside the media folders
		}
	}
	perms := p.User.Permissions
	if !p.User.IsAdmin && ((plan.TranscodeVideo && !perms.VideoTranscoding) ||
		(plan.TranscodeAudio && !perms.AudioTranscoding) || (plan.Method == "remux" && !perms.Remux)) {
		return Info{}, ErrTranscodeDenied
	}
	info := Info{FileID: f.ID, Method: plan.Method, StartMS: start, DurationMS: f.DurationMS, Audio: f.Audio,
		AudioIndex: audio, Reason: plan.Reason, BurnedSubtitle: -1}
	if plan.Burn != nil {
		info.BurnedSubtitle = plan.Burn.Index
	}
	if plan.Method == "direct" {
		scope := fmt.Sprintf("/stream/file/%s/", f.ID)
		info.URL = scope + "video" + filepath.Ext(f.Path) + "?" + s.Signer.Query(scope, URLTTL)
	} else {
		path, err := s.FilePath(ctx, f.ID)
		if err != nil {
			return Info{}, err
		}
		job := Job{Input: path, StartMS: start, AudioIndex: audio, Plan: plan, HEVC: strings.EqualFold(f.VideoCodec, "hevc")}
		if !p.User.IsAdmin {
			job.Owner = p.User.ID
		}
		if p.SessionID != uuid.Nil {
			// One stream per signed-in device: a restart (seek, track switch) replaces it.
			job.Client = p.SessionID.String()
		}
		id, err := s.HLS.Start(ctx, job)
		if err != nil {
			return Info{}, fmt.Errorf("%w: %w", ErrTranscoderFailure, err)
		}
		scope := "/stream/hls/" + id + "/"
		info.URL = scope + "index.m3u8?" + s.Signer.Query(scope, URLTTL)
	}
	subScope := fmt.Sprintf("/stream/sub/%s/", f.ID)
	q := s.Signer.Query(subScope, URLTTL)
	for i, sub := range f.Subtitles {
		if IsBitmapSubtitle(sub.Codec) {
			// Pictures cannot become WebVTT: offered for burn-in instead.
			info.BitmapSubtitles = append(info.BitmapSubtitles, BitmapSubtitle{Index: i, Codec: sub.Codec, Language: sub.Language, Forced: sub.Forced})
			continue
		}
		if !IsTextSubtitle(sub.Codec) {
			continue
		}
		info.Subtitles = append(info.Subtitles, Subtitle{Index: i, Language: sub.Language, Forced: sub.Forced,
			External: sub.External, URL: fmt.Sprintf("%s%d.vtt?%s", subScope, i, q)})
	}
	choices := make([]library.SubtitleChoice, len(info.Subtitles))
	for i, sub := range info.Subtitles {
		choices[i] = library.SubtitleChoice{Language: sub.Language, Forced: sub.Forced}
	}
	audioLang := ""
	if audio >= 0 && audio < len(f.Audio) {
		audioLang = f.Audio[audio].Language
	}
	info.SubtitleDefault = library.PickSubtitle(choices, audioLang, prefs)
	switch {
	case info.BurnedSubtitle >= 0:
		info.SubtitleDefault = -1 // already in the picture
	case req.SubtitleIndex != nil:
		// The viewer's own choice of a text track (or none) wins over the preference.
		info.SubtitleDefault = -1
		for i, sub := range info.Subtitles {
			if sub.Index == *req.SubtitleIndex {
				info.SubtitleDefault = i
			}
		}
	}
	info.Chapters, info.Segments, info.Trickplay = s.analysis(ctx, f.ID)
	return info, nil
}

// subtitleChoice returns the subtitle track to consider for burn-in: the
// viewer's choice, or, when none was made, a forced bitmap track that the
// subtitle preference picks (forced subtitles translate signs and foreign
// dialogue and belong in the picture whatever the device).
func (s *Server) subtitleChoice(f library.MediaFile, audio int, prefs library.Prefs, chosen *int) int {
	if chosen != nil {
		return *chosen
	}
	choices := make([]library.SubtitleChoice, len(f.Subtitles))
	for i, sub := range f.Subtitles {
		choices[i] = library.SubtitleChoice{Language: sub.Language, Forced: sub.Forced}
	}
	audioLang := ""
	if audio >= 0 && audio < len(f.Audio) {
		audioLang = f.Audio[audio].Language
	}
	if i := library.PickSubtitle(choices, audioLang, prefs); i >= 0 && f.Subtitles[i].Forced && IsBitmapSubtitle(f.Subtitles[i].Codec) {
		return i
	}
	return -1
}

// sidecarPath validates a sidecar subtitle file; VobSub is read through its .idx.
func sidecarPath(roots library.Roots, sub library.SubtitleStream) (string, error) {
	if strings.EqualFold(filepath.Ext(sub.Path), ".sub") {
		if p, err := library.ResolveSafePath(roots, strings.TrimSuffix(sub.Path, filepath.Ext(sub.Path))+".idx"); err == nil {
			if _, err := os.Stat(p); err == nil {
				return p, nil
			}
		}
	}
	return library.ResolveSafePath(roots, sub.Path)
}

// pickFile returns the requested version, or the best one, that is still on disk. A
// version that disappeared since the last scan (moved by an *arr app, deleted) is
// skipped, so another copy of the same item still plays. With none left the error
// wraps os.ErrNotExist.
func pickFile(files []library.MediaFile, want *uuid.UUID, exists func(path string) bool) (library.MediaFile, error) {
	if want != nil {
		for _, x := range files {
			if x.ID == *want && exists(x.Path) {
				return x, nil
			}
		}
	}
	for _, x := range files { // best version first (highest resolution)
		if exists(x.Path) {
			return x, nil
		}
	}
	return library.MediaFile{}, fmt.Errorf("no version of this item is on disk: %w", os.ErrNotExist)
}
