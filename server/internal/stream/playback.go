package stream

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
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
	f := files[0] // best version first (highest resolution)
	if req.FileID != nil {
		found := false
		for _, x := range files {
			if x.ID == *req.FileID {
				f, found = x, true
			}
		}
		if !found {
			return Info{}, library.ErrNotFound
		}
	}
	audio := 0
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

	plan := Decide(f, audio, req.Device)
	perms := p.User.Permissions
	if !p.User.IsAdmin && ((plan.TranscodeVideo && !perms.VideoTranscoding) ||
		(plan.TranscodeAudio && !perms.AudioTranscoding) || (plan.Method == "remux" && !perms.Remux)) {
		return Info{}, ErrTranscodeDenied
	}
	info := Info{FileID: f.ID, Method: plan.Method, StartMS: start, DurationMS: f.DurationMS, Audio: f.Audio,
		AudioIndex: audio, Reason: plan.Reason}
	if plan.Method == "direct" {
		scope := fmt.Sprintf("/stream/file/%s/", f.ID)
		info.URL = scope + "video" + filepath.Ext(f.Path) + "?" + s.Signer.Query(scope, URLTTL)
	} else {
		path, err := s.FilePath(ctx, f.ID)
		if err != nil {
			return Info{}, err
		}
		id, err := s.HLS.Start(ctx, Job{Input: path, StartMS: start, AudioIndex: audio, Plan: plan})
		if err != nil {
			return Info{}, fmt.Errorf("%w: %w", ErrTranscoderFailure, err)
		}
		scope := "/stream/hls/" + id + "/"
		info.URL = scope + "index.m3u8?" + s.Signer.Query(scope, URLTTL)
	}
	subScope := fmt.Sprintf("/stream/sub/%s/", f.ID)
	q := s.Signer.Query(subScope, URLTTL)
	for i, sub := range f.Subtitles {
		if !sub.External && !textSubtitle(sub.Codec) {
			continue // bitmap subtitles (PGS/VobSub) need burn-in, coming with the transcoder options
		}
		info.Subtitles = append(info.Subtitles, Subtitle{Index: i, Language: sub.Language, Forced: sub.Forced,
			External: sub.External, URL: fmt.Sprintf("%s%d.vtt?%s", subScope, i, q)})
	}
	return info, nil
}

func textSubtitle(codec string) bool {
	switch codec {
	case "subrip", "srt", "ass", "ssa", "webvtt", "vtt", "mov_text", "text":
		return true
	}
	return false
}
