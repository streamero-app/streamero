package stream

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/books"
)

// FileURL is a signed link to a media file as it is (books, photos, downloads).
func (s *Server) FileURL(fileID uuid.UUID, path string) string {
	scope := fmt.Sprintf("/stream/file/%s/", fileID)
	return scope + "file" + strings.ToLower(filepath.Ext(path)) + "?" + s.Signer.Query(scope, URLTTL)
}

// PageURLs are signed links to the pages of a comic archive (one signature for all pages).
func (s *Server) PageURLs(fileID uuid.UUID, count int) []string {
	scope := fmt.Sprintf("/stream/page/%s/", fileID)
	q := s.Signer.Query(scope, URLTTL)
	out := make([]string, count)
	for i := range out {
		out[i] = scope + strconv.Itoa(i) + "?" + q
	}
	return out
}

// browserImages can be shown as they are by every client.
var browserImages = map[string]string{".jpg": "image/jpeg", ".jpeg": "image/jpeg", ".png": "image/png",
	".gif": "image/gif", ".webp": "image/webp"}

// PhotoURL is a signed link to a picture every client can show: the file itself, or
// a JPEG made from it (HEIC/HEIF and AVIF, which not every browser or OS shows).
func (s *Server) PhotoURL(fileID uuid.UUID, path string) (string, string) {
	ext := strings.ToLower(filepath.Ext(path))
	if t, ok := browserImages[ext]; ok {
		return s.FileURL(fileID, path), t
	}
	scope := fmt.Sprintf("/stream/photo/%s/", fileID)
	return scope + "view.jpg?" + s.Signer.Query(scope, URLTTL), "image/jpeg"
}

// page serves one page of a comic archive, read without extracting the archive.
func (s *Server) page(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "fileId"))
	n, nerr := strconv.Atoi(chi.URLParam(r, "n"))
	if err != nil || nerr != nil || n < 0 {
		http.NotFound(w, r)
		return
	}
	p, err := s.FilePath(r.Context(), id)
	if err != nil || !books.IsComic(books.FormatOf(p)) {
		http.NotFound(w, r)
		return
	}
	a, err := books.OpenArchive(p)
	if err != nil {
		http.Error(w, "unreadable archive", http.StatusUnprocessableEntity)
		return
	}
	defer func() { _ = a.Close() }()
	data, typ, err := books.ReadPage(a, n)
	if err != nil {
		if errors.Is(err, books.ErrNoSuchPage) {
			http.NotFound(w, r)
			return
		}
		http.Error(w, "unreadable page", http.StatusUnprocessableEntity)
		return
	}
	w.Header().Set("Content-Type", typ)
	w.Header().Set("X-Content-Type-Options", "nosniff")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	_, _ = w.Write(data)
}

// photo serves a picture converted to JPEG (cached under PhotoCache).
func (s *Server) photo(w http.ResponseWriter, r *http.Request) {
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
	if _, ok := browserImages[strings.ToLower(filepath.Ext(p))]; ok || s.PhotoCache == "" || s.FFmpeg == "" {
		s.ServeFile(w, r, id)
		return
	}
	// The cache name is built from the parsed UUID only, never from request text.
	dst := filepath.Join(s.PhotoCache, id.String()+".jpg")
	if !fresh(p, dst) {
		if err := s.convertPhoto(r.Context(), p, dst); err != nil {
			http.Error(w, "could not convert photo", http.StatusUnprocessableEntity)
			return
		}
	}
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeFile(w, r, dst) //nolint:gosec // G703: dst is PhotoCache/<uuid>.jpg
}

func fresh(src, dst string) bool {
	a, err1 := os.Stat(src)
	b, err2 := os.Stat(dst) //nolint:gosec // G703: cache path built from a UUID
	return err1 == nil && err2 == nil && !a.ModTime().After(b.ModTime())
}

// photoSlots caps how many photo conversions run at once on the whole server: an
// album of HEIC pictures opened on several devices must not start an ffmpeg per
// picture (the transcode caps of D-044 only count streaming sessions).
var photoSlots = make(chan struct{}, 4)

// convertPhoto writes a JPEG of at most 3840 px (the image's own rotation applied).
func (s *Server) convertPhoto(ctx context.Context, src, dst string) error {
	select {
	case photoSlots <- struct{}{}:
		defer func() { <-photoSlots }()
	case <-ctx.Done():
		return ctx.Err()
	}
	if fresh(src, dst) {
		return nil // converted by another request while this one waited
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil { //nolint:gosec // G703: cache path built from a UUID
		return err
	}
	tmp := dst + ".tmp.jpg"
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// #nosec G204 -- fixed binary and flags; src was validated by ResolveSafePath.
	cmd := exec.CommandContext(cctx, s.FFmpeg, "-nostdin", "-v", "error", "-y", "-i", src, "-frames:v", "1",
		"-vf", "scale='min(3840,iw)':-2", "-q:v", "2", tmp)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp) //nolint:gosec // G703: cache path built from a UUID
		return err
	}
	return os.Rename(tmp, dst) //nolint:gosec // G703: cache path built from a UUID
}
