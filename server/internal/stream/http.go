package stream

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/library"
)

// Server serves /stream/* (outside the OpenAPI surface; see D-015).
type Server struct {
	Pool    *pgxpool.Pool
	Roots   library.Roots
	Signer  Signer
	HLS     *Manager
	FFmpeg  string
	SubsTTL time.Duration
	// Downloads serves /stream/dl/{id}/{name}: offline download files (D-040).
	Downloads http.Handler
	// Images serves /stream/img/{key}/{name} (live TV logos and programme images; optional).
	Images http.Handler
}

// Routes returns the handler for /stream.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.verify)
	r.Get("/file/{fileId}/{name}", s.file)
	r.Get("/hls/{session}/{name}", s.hls)
	r.Get("/sub/{fileId}/{track}", s.subtitle)
	r.Get("/orig/{fileId}/{name}", s.original)
	r.Get("/dl/{id}/{name}", func(w http.ResponseWriter, r *http.Request) {
		if s.Downloads == nil {
			http.NotFound(w, r)
			return
		}
		s.Downloads.ServeHTTP(w, r)
	})
	if s.Images != nil {
		r.Handle("/img/{key}/{name}", s.Images)
	}
	return r
}

// verify rejects requests without a valid signature for their scope.
func (s *Server) verify(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		full := r.URL.Path
		if !strings.HasPrefix(full, "/stream/") {
			full = "/stream" + full // mounted with the prefix stripped
		}
		scope := ScopeOf(full)
		if scope == "" || !s.Signer.Valid(scope, r.URL.Query()) {
			http.Error(w, "invalid or expired stream link", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// FilePath returns the validated path of a media file.
func (s *Server) FilePath(ctx context.Context, fileID uuid.UUID) (string, error) {
	var p string
	if err := s.Pool.QueryRow(ctx, `SELECT path FROM media_files WHERE id = $1`, fileID).Scan(&p); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", library.ErrNotFound
		}
		return "", err
	}
	// The database path must still resolve inside the media roots (symlinks swapped since the scan, etc.).
	return library.ResolveSafePath(s.Roots, p)
}

func (s *Server) file(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	s.ServeFile(w, r, id)
}

// ServeFile sends a media file with Range support. The caller has authorised
// the request; the stored path is re-validated against the media roots.
func (s *Server) ServeFile(w http.ResponseWriter, r *http.Request, id uuid.UUID) {
	p, err := s.FilePath(r.Context(), id)
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
	w.Header().Set("Cache-Control", "private, max-age=3600")
	http.ServeContent(w, r, filepath.Base(p), st.ModTime(), f) // Range requests for seeking
}

func (s *Server) hls(w http.ResponseWriter, r *http.Request) {
	id, name := chi.URLParam(r, "session"), chi.URLParam(r, "name")
	if name == "index.m3u8" {
		q := r.URL.Query()
		body, err := s.HLS.Playlist(id, "exp="+q.Get("exp")+"&sig="+q.Get("sig"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/vnd.apple.mpegurl")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		_, _ = w.Write(body) // #nosec G705 -- ffmpeg-generated HLS playlist served as mpegurl, not HTML
		return
	}
	p, err := s.HLS.File(id, name)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if strings.HasSuffix(name, ".m4s") || strings.HasSuffix(name, ".mp4") {
		w.Header().Set("Content-Type", "video/mp4")
	}
	http.ServeFile(w, r, p)
}

// subtitle converts an embedded or external subtitle track to WebVTT ("<index>.vtt")
// or SubRip ("<index>.srt", the sidecar of offline downloads).
func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	name, format, ok := strings.Cut(chi.URLParam(r, "track"), ".")
	if !ok || (format != "vtt" && format != "srt") {
		http.NotFound(w, r)
		return
	}
	index, err := strconv.Atoi(name)
	if err != nil || index < 0 {
		http.NotFound(w, r)
		return
	}
	// offsetMs (not part of the signature) shifts WebVTT cues for a transcoded
	// stream that starts mid-way: its time 0 is the file's offsetMs.
	var offset int64
	if v := r.URL.Query().Get("offsetMs"); v != "" && format == "vtt" {
		offset, err = strconv.ParseInt(v, 10, 64)
		if err != nil || offset < 0 {
			http.Error(w, "invalid offsetMs", http.StatusBadRequest)
			return
		}
		var duration int64
		if s.Pool.QueryRow(r.Context(), `SELECT COALESCE(duration_ms, 0) FROM media_files WHERE id = $1`, id).Scan(&duration) == nil && duration > 0 {
			offset = min(offset, duration)
		}
	}
	s.serveSubtitle(w, r, id, index, format, offset)
}

// original sends a media file as an attachment ("Download original" on the web, D-040);
// the link is only handed out to accounts with the download permission.
func (s *Server) original(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Disposition", Attachment(chi.URLParam(r, "name")))
	s.ServeFile(w, r, id)
}

// Attachment returns a Content-Disposition value that saves a file under name
// (ASCII fallback plus the RFC 5987 UTF-8 form).
func Attachment(name string) string {
	ascii := strings.Map(func(c rune) rune {
		if c < 0x20 || c > 0x7e || c == '"' || c == '\\' || c == '/' {
			return '_'
		}
		return c
	}, name)
	return `attachment; filename="` + ascii + `"; filename*=UTF-8''` + strings.ReplaceAll(url.QueryEscape(name), "+", "%20")
}

// subtitleFormats maps requested extensions to an ffmpeg muxer and a content type.
var subtitleFormats = map[string][2]string{
	"vtt": {"webvtt", "text/vtt; charset=utf-8"}, "webvtt": {"webvtt", "text/vtt; charset=utf-8"},
	"srt": {"srt", "application/x-subrip; charset=utf-8"}, "subrip": {"srt", "application/x-subrip; charset=utf-8"},
	"ass": {"ass", "text/x-ssa; charset=utf-8"}, "ssa": {"ass", "text/x-ssa; charset=utf-8"},
}

// ServeSubtitle converts subtitle track index (position in the file's subtitle
// list) to format (vtt, srt or ass). The caller has authorised the request.
func (s *Server) ServeSubtitle(w http.ResponseWriter, r *http.Request, id uuid.UUID, index int, format string) {
	s.serveSubtitle(w, r, id, index, format, 0)
}

// serveSubtitle is ServeSubtitle with WebVTT cues moved offsetMs earlier (see ShiftVTT).
func (s *Server) serveSubtitle(w http.ResponseWriter, r *http.Request, id uuid.UUID, index int, format string, offsetMs int64) {
	muxer, ok := subtitleFormats[strings.ToLower(format)]
	if !ok || index < 0 {
		http.NotFound(w, r)
		return
	}
	video, err := s.FilePath(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	subs, err := loadSubtitles(r.Context(), s.Pool, id)
	if err != nil || index >= len(subs) {
		http.NotFound(w, r)
		return
	}
	sub := subs[index]
	var args []string
	if sub.External {
		ext, err := library.ResolveSafePath(s.Roots, sub.Path)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		args = []string{"-nostdin", "-loglevel", "error", "-i", ext, "-f", muxer[0], "-"}
	} else {
		// Embedded tracks are counted among the file's subtitle streams.
		embedded := 0
		for _, x := range subs[:index] {
			if !x.External {
				embedded++
			}
		}
		args = []string{"-nostdin", "-loglevel", "error", "-i", video, "-map", "0:s:" + strconv.Itoa(embedded), "-f", muxer[0], "-"}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.FFmpeg, args...).Output() // #nosec G204 -- validated paths, fixed binary
	if err != nil {
		http.Error(w, "subtitle conversion failed", http.StatusUnprocessableEntity)
		return
	}
	if muxer[0] == "webvtt" {
		out = ShiftVTT(out, offsetMs)
	}
	w.Header().Set("Content-Type", muxer[1])
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(out)
}
