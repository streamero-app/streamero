package metadata

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Local artwork: images the user keeps next to their media, by the common naming
// conventions. They always win over downloaded or grabbed artwork, because the user
// put them there on purpose.
var localNames = []struct {
	kind   string // cache kind (poster, backdrop, logo)
	column string
	width  int
	png    bool
	names  []string // folder-level names; "<stem>-<name>" for a loose file
}{
	{"poster", "has_poster", 1000, false, []string{"poster", "folder", "cover"}},
	{"backdrop", "has_backdrop", 1920, false, []string{"backdrop", "fanart", "background"}},
	{"logo", "has_logo", 800, true, []string{"logo", "clearlogo"}},
}

var localExts = []string{".jpg", ".jpeg", ".png", ".webp"}

// LocalArtwork imports poster/backdrop/logo files found beside movies and series.
// A file is (re)imported when it is newer than the cached copy, so replacing a
// poster on disk shows up on the next scan.
func (r *Refresher) LocalArtwork(ctx context.Context, libraryID uuid.UUID) (int, error) {
	if r.ImageDir == "" || r.ResolvePath == nil {
		return 0, nil
	}
	rows, err := r.Pool.Query(ctx, `SELECT id, path FROM items
		WHERE library_id = $1 AND parent_id IS NULL AND kind IN ('movie', 'series')`, libraryID)
	if err != nil {
		return 0, err
	}
	type item struct {
		id   uuid.UUID
		path string
	}
	var items []item
	for rows.Next() {
		var it item
		if err := rows.Scan(&it.id, &it.path); err != nil {
			rows.Close()
			return 0, err
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	done := 0
	for _, it := range items {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		host, err := r.ResolvePath(it.path)
		if err != nil {
			continue
		}
		dir, prefix := host, ""
		if st, err := os.Stat(host); err != nil {
			continue
		} else if !st.IsDir() {
			dir = filepath.Dir(host)
			prefix = strings.TrimSuffix(filepath.Base(host), filepath.Ext(host)) + "-"
		}
		changed := false
		for _, ln := range localNames {
			dst := ImagePath(r.ImageDir, it.id, ln.kind)
			marker := LocalMarker(dst)
			src := findLocal(dir, prefix, ln.names)
			if src == "" {
				// The user removed their file: let downloaded artwork take over again.
				_ = os.Remove(marker)
				continue
			}
			if !newer(src, marker) {
				continue
			}
			if !r.importImage(ctx, src, dst, ln.width, ln.png) || os.WriteFile(marker, nil, 0o600) != nil {
				continue
			}
			// column is one of the constants above, never input.
			q := fmt.Sprintf(`UPDATE items SET %s = true, colors = NULL WHERE id = $1`, ln.column)
			if _, err := r.Pool.Exec(ctx, q, it.id); err != nil {
				return done, err
			}
			changed = true
		}
		if changed {
			done++
		}
	}
	return done, nil
}

// LocalMarker sits next to a cached image that came from the user's own file;
// downloads never overwrite such an image.
func LocalMarker(cached string) string { return cached + ".local" }

// findLocal returns the first existing artwork file in dir. Only fixed names are
// tried, so nothing outside the item's own folder is ever read.
func findLocal(dir, prefix string, names []string) string {
	for _, n := range names {
		for _, ext := range localExts {
			p := filepath.Join(dir, prefix+n+ext)
			if st, err := os.Stat(p); err == nil && st.Mode().IsRegular() && st.Size() > 0 {
				return p
			}
		}
	}
	return ""
}

func newer(src, dst string) bool {
	s, err := os.Stat(src)
	if err != nil {
		return false
	}
	d, err := os.Stat(dst)
	return err != nil || s.ModTime().After(d.ModTime())
}

// importImage scales a local image to a sensible size (posters straight from a
// designer can be 15 MB) and writes it to the cache. Without ffmpeg it copies.
func (r *Refresher) importImage(ctx context.Context, src, dst string, width int, png bool) bool {
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return false
	}
	tmp := dst + ".tmp"
	if r.FFmpeg == "" {
		data, err := os.ReadFile(src) //nolint:gosec // G304: fixed artwork name inside the item's resolved folder
		if err != nil || os.WriteFile(tmp, data, 0o600) != nil {
			return false
		}
		return os.Rename(tmp, dst) == nil
	}
	codec := []string{"-q:v", "3", "-f", "mjpeg"}
	if png {
		codec = []string{"-f", "image2", "-c:v", "png"}
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	args := append([]string{"-nostdin", "-v", "error", "-y", "-i", src, "-frames:v", "1",
		"-vf", fmt.Sprintf("scale='min(%d,iw)':-2", width)}, codec...)
	args = append(args, tmp)
	cmd := exec.CommandContext(cctx, r.FFmpeg, args...) //nolint:gosec // G204: fixed flags, path from ResolvePath
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		r.Log.Debug("local artwork import failed", "src", src, "err", err)
		return false
	}
	return os.Rename(tmp, dst) == nil
}
