package metadata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/books"
)

// thumbWidth is the size of generated book covers and photo thumbnails (longest side).
const thumbWidth = 720

var bookCoverNames = []string{"cover", "folder", "front", "poster"}

// MediaArtwork gives books, audiobooks, photos, videos and photo albums their
// pictures from the files themselves: the EPUB cover, the first comic page, the
// first PDF page, embedded audiobook art (or a cover.jpg beside it), a photo
// thumbnail, a frame of a video, and an album's newest photo. Each item is tried
// once (art_grabbed_at); a changed file gets a new item row and is tried again.
func (r *Refresher) MediaArtwork(ctx context.Context, libraryID uuid.UUID) (int, error) {
	if r.ImageDir == "" || r.ResolvePath == nil {
		return 0, nil
	}
	type job struct {
		id               uuid.UUID
		kind, path, meta string
		durMS            int64
	}
	done := 0
	for {
		rows, err := r.Pool.Query(ctx, `SELECT i.id, i.kind, f.path, COALESCE(i.media_meta::text, ''), COALESCE(f.duration_ms, 0)
			FROM items i
			JOIN LATERAL (SELECT path, duration_ms FROM media_files m WHERE m.item_id = i.id ORDER BY m.path LIMIT 1) f ON true
			WHERE i.library_id = $1 AND i.art_grabbed_at IS NULL AND NOT i.has_poster
				AND i.kind IN ('book', 'audiobook', 'photo', 'video')
			LIMIT 500`, libraryID)
		if err != nil {
			return done, err
		}
		var jobs []job
		for rows.Next() {
			var j job
			if err := rows.Scan(&j.id, &j.kind, &j.path, &j.meta, &j.durMS); err != nil {
				rows.Close()
				return done, err
			}
			jobs = append(jobs, j)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return done, err
		}
		if len(jobs) == 0 {
			break
		}
		var mu sync.Mutex
		var wg sync.WaitGroup
		ch := make(chan job)
		var firstErr error
		for range 4 {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for j := range ch {
					ok := r.mediaPicture(ctx, j.kind, j.path, j.meta, j.durMS, ImagePath(r.ImageDir, j.id, "poster"))
					_, err := r.Pool.Exec(ctx, `UPDATE items SET art_grabbed_at = now(), has_poster = has_poster OR $2,
						colors = CASE WHEN $2 THEN NULL ELSE colors END WHERE id = $1`, j.id, ok)
					mu.Lock()
					if err != nil && firstErr == nil {
						firstErr = err
					}
					if ok {
						done++
					}
					mu.Unlock()
				}
			}()
		}
		for _, j := range jobs {
			if ctx.Err() != nil {
				break
			}
			ch <- j
		}
		close(ch)
		wg.Wait()
		if firstErr != nil {
			return done, firstErr
		}
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
	}
	// Albums show their newest photo (re-chosen when it changes).
	_, err := r.Pool.Exec(ctx, `UPDATE items a SET has_poster = false WHERE a.library_id = $1 AND a.kind = 'photoalbum'
		AND a.has_poster AND NOT EXISTS (SELECT 1 FROM items c WHERE c.parent_id = a.id AND c.has_poster)`, libraryID)
	if err != nil {
		return done, err
	}
	rows, err := r.Pool.Query(ctx, `WITH RECURSIVE tree AS (
			SELECT a.id AS album, a.id AS node FROM items a WHERE a.library_id = $1 AND a.kind = 'photoalbum'
			UNION ALL SELECT t.album, c.id FROM tree t JOIN items c ON c.parent_id = t.node AND c.kind = 'photoalbum')
		SELECT DISTINCT ON (t.album) t.album, p.id FROM tree t
		JOIN items p ON p.parent_id = t.node AND p.kind IN ('photo', 'video') AND p.has_poster
		ORDER BY t.album, p.taken_at DESC NULLS LAST`, libraryID)
	if err != nil {
		return done, err
	}
	type pick struct{ album, photo uuid.UUID }
	var picks []pick
	for rows.Next() {
		var p pick
		if err := rows.Scan(&p.album, &p.photo); err != nil {
			rows.Close()
			return done, err
		}
		picks = append(picks, p)
	}
	rows.Close()
	for _, p := range picks {
		src, dst := ImagePath(r.ImageDir, p.photo, "poster"), ImagePath(r.ImageDir, p.album, "poster")
		if !newer(src, dst) {
			continue
		}
		data, err := os.ReadFile(src) //nolint:gosec // G304: cache path built from a UUID
		if err != nil || os.MkdirAll(filepath.Dir(dst), 0o750) != nil || os.WriteFile(dst, data, 0o600) != nil { //nolint:gosec // G703: cache path built from a UUID
			continue
		}
		if _, err := r.Pool.Exec(ctx, `UPDATE items SET has_poster = true, colors = NULL WHERE id = $1`, p.album); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// mediaPicture writes the poster of one item; false when there is none.
func (r *Refresher) mediaPicture(ctx context.Context, kind, path, rawMeta string, durMS int64, dst string) bool {
	src, err := r.ResolvePath(path)
	if err != nil {
		return false
	}
	switch kind {
	case "book":
		if books.FormatOf(src) == "pdf" {
			return r.pdfCover(ctx, src, dst)
		}
		data, err := books.Cover(src)
		if err != nil || len(data) == 0 {
			return false
		}
		return r.imageBytes(ctx, data, dst)
	case "audiobook":
		dir := filepath.Dir(src)
		if d := strings.ToLower(filepath.Base(dir)); strings.HasPrefix(d, "cd") || strings.HasPrefix(d, "disc") {
			dir = filepath.Dir(dir)
		}
		if local := findLocal(dir, "", bookCoverNames); local != "" {
			return r.importImage(ctx, local, dst, 1000, false)
		}
		return r.embeddedCover(ctx, src, dst)
	case "photo":
		orientation := 0
		var m struct {
			Photo *struct {
				Orientation int `json:"orientation"`
			} `json:"photo"`
		}
		if json.Unmarshal([]byte(rawMeta), &m) == nil && m.Photo != nil {
			orientation = m.Photo.Orientation
		}
		return r.thumbnail(ctx, src, dst, orientation)
	case "video":
		return r.grab(ctx, path, durMS, false, dst)
	}
	return false
}

// orientationFilter undoes EXIF orientation 2–8 (ffmpeg's own rotation is turned off).
func orientationFilter(o int) string {
	switch o {
	case 2:
		return "hflip,"
	case 3:
		return "hflip,vflip,"
	case 4:
		return "vflip,"
	case 5:
		return "transpose=0,"
	case 6:
		return "transpose=1,"
	case 7:
		return "transpose=3,"
	case 8:
		return "transpose=2,"
	}
	return ""
}

func (r *Refresher) runFFmpeg(ctx context.Context, dst string, stdin []byte, args ...string) bool {
	if r.FFmpeg == "" || os.MkdirAll(filepath.Dir(dst), 0o750) != nil {
		return false
	}
	tmp := dst + ".tmp.jpg"
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	full := []string{"-v", "error", "-y"}
	if stdin == nil {
		full = append(full, "-nostdin")
	}
	full = append(full, args...)
	full = append(full, "-frames:v", "1", "-q:v", "3", "-f", "mjpeg", tmp)
	cmd := exec.CommandContext(cctx, r.FFmpeg, full...) //nolint:gosec // G204: fixed flags, paths from ResolvePath
	if stdin != nil {
		cmd.Stdin = bytes.NewReader(stdin)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		_ = os.Remove(tmp)
		r.Log.Debug("thumbnail failed", "dst", dst, "err", err, "out", string(out))
		return false
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() < 100 {
		_ = os.Remove(tmp)
		return false
	}
	return os.Rename(tmp, dst) == nil
}

var scaleLongest = fmt.Sprintf("scale='if(gt(iw,ih),min(%[1]d,iw),-2)':'if(gt(iw,ih),-2,min(%[1]d,ih))'", thumbWidth)

// thumbnail scales a photo; JPEG/PNG/WebP orientation comes from EXIF, HEIC/AVIF
// rotation is left to ffmpeg (their container carries it).
func (r *Refresher) thumbnail(ctx context.Context, src, dst string, orientation int) bool {
	ext := strings.ToLower(filepath.Ext(src))
	if ext == ".heic" || ext == ".heif" || ext == ".avif" {
		return r.runFFmpeg(ctx, dst, nil, "-i", src, "-vf", scaleLongest)
	}
	return r.runFFmpeg(ctx, dst, nil, "-noautorotate", "-i", src, "-vf", orientationFilter(orientation)+scaleLongest)
}

// imageBytes scales an image held in memory (a cover read from an archive).
func (r *Refresher) imageBytes(ctx context.Context, data []byte, dst string) bool {
	if r.FFmpeg == "" {
		// Without ffmpeg a JPEG cover is kept as it is.
		if len(data) > 3 && data[0] == 0xFF && data[1] == 0xD8 && os.MkdirAll(filepath.Dir(dst), 0o750) == nil {
			return os.WriteFile(dst, data, 0o600) == nil
		}
		return false
	}
	return r.runFFmpeg(ctx, dst, data, "-i", "pipe:0", "-vf", scaleLongest)
}

// pdfCover renders the first page of a PDF with pdftoppm (poppler-utils), when installed.
func (r *Refresher) pdfCover(ctx context.Context, src, dst string) bool {
	bin := r.Pdftoppm
	if bin == "" {
		var err error
		if bin, err = exec.LookPath("pdftoppm"); err != nil {
			return false
		}
	}
	if os.MkdirAll(filepath.Dir(dst), 0o750) != nil {
		return false
	}
	base := dst + ".tmp"
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "-jpeg", "-f", "1", "-l", "1", "-singlefile", //nolint:gosec // G204: fixed flags, path from ResolvePath
		"-scale-to", fmt.Sprint(thumbWidth), src, base) // src is absolute: never read as a flag
	if err := cmd.Run(); err != nil {
		_ = os.Remove(base + ".jpg")
		return false
	}
	return os.Rename(base+".jpg", dst) == nil
}
