package stream

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
)

// StartAdult prepares playback of an adult scene's files (D-053). The caller has
// passed the adult access gate. It hands out signed URLs like Start, but under
// /stream/adult/ (files of adult_files only); it reports nothing to the activity
// dashboard and offers no subtitles, chapters or trickplay.
func (s *Server) StartAdult(ctx context.Context, p auth.Principal, files []library.MediaFile, resumeMS int64, req Request) (Info, error) {
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
	audio := 0
	if req.AudioIndex != nil && *req.AudioIndex >= 0 && *req.AudioIndex < len(f.Audio) {
		audio = *req.AudioIndex
	}
	if len(f.Audio) == 0 {
		audio = -1
	}
	start := resumeMS
	if req.StartMS != nil {
		start = max(0, *req.StartMS)
	}
	plan := Decide(f, audio, req.Device)
	perms := p.User.Permissions
	if !p.User.IsAdmin && ((plan.TranscodeVideo && !perms.VideoTranscoding) ||
		(plan.TranscodeAudio && !perms.AudioTranscoding) || (plan.Method == "remux" && !perms.Remux)) {
		return Info{}, ErrTranscodeDenied
	}
	info := Info{FileID: f.ID, Method: plan.Method, StartMS: start, DurationMS: f.DurationMS, Audio: f.Audio,
		AudioIndex: audio, Reason: plan.Reason, BurnedSubtitle: -1, SubtitleDefault: -1}
	if plan.Method == "direct" {
		scope := fmt.Sprintf("/stream/adult/%s/", f.ID)
		info.URL = scope + "video" + strings.ToLower(filepath.Ext(f.Path)) + "?" + s.Signer.Query(scope, URLTTL)
		return info, nil
	}
	path, err := library.ResolveSafePath(s.Roots, f.Path)
	if err != nil {
		return Info{}, library.ErrNotFound
	}
	job := Job{Input: path, StartMS: start, AudioIndex: audio, Plan: plan, HEVC: strings.EqualFold(f.VideoCodec, "hevc")}
	if !p.User.IsAdmin {
		job.Owner = p.User.ID
	}
	if p.SessionID != uuid.Nil {
		job.Client = p.SessionID.String()
	}
	id, err := s.HLS.Start(ctx, job)
	if err != nil {
		return Info{}, fmt.Errorf("%w: %w", ErrTranscoderFailure, err)
	}
	scope := "/stream/hls/" + id + "/"
	info.URL = scope + "index.m3u8?" + s.Signer.Query(scope, URLTTL)
	return info, nil
}

// adultFile serves /stream/adult/{fileId}/{name}: a file of an adult library,
// only through a signed link handed out by StartAdult.
func (s *Server) adultFile(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil || s.AdultPath == nil {
		http.NotFound(w, r)
		return
	}
	stored, err := s.AdultPath(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := library.ResolveSafePath(s.Roots, stored)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	f, err := os.Open(p) // #nosec G304 -- validated by ResolveSafePath
	if err != nil {
		http.NotFound(w, r)
		return
	}
	defer func() { _ = f.Close() }()
	st, err := f.Stat()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	// Never kept in a shared or disk cache.
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Referrer-Policy", "no-referrer")
	http.ServeContent(w, r, "video"+filepath.Ext(p), st.ModTime(), f)
}
