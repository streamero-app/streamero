package stream

import (
	"context"
	"errors"
	"net/http"
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
}

// Routes returns the handler for /stream.
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(s.verify)
	r.Get("/file/{fileId}/{name}", s.file)
	r.Get("/hls/{session}/{name}", s.hls)
	r.Get("/sub/{fileId}/{index}.vtt", s.subtitle)
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

// subtitle converts an embedded or external subtitle track to WebVTT.
func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	index, err := strconv.Atoi(chi.URLParam(r, "index"))
	if err != nil || index < 0 {
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
		args = []string{"-nostdin", "-loglevel", "error", "-i", ext, "-f", "webvtt", "-"}
	} else {
		// Embedded tracks are counted among the file's subtitle streams.
		embedded := 0
		for _, x := range subs[:index] {
			if !x.External {
				embedded++
			}
		}
		args = []string{"-nostdin", "-loglevel", "error", "-i", video, "-map", "0:s:" + strconv.Itoa(embedded), "-f", "webvtt", "-"}
	}
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	out, err := exec.CommandContext(ctx, s.FFmpeg, args...).Output() // #nosec G204 -- validated paths, fixed binary
	if err != nil {
		http.Error(w, "subtitle conversion failed", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", "text/vtt; charset=utf-8")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	_, _ = w.Write(out)
}
