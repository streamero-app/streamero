package metadata

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/google/uuid"
)

var albumCoverNames = []string{"cover", "folder", "front", "album", "albumart"}
var artistImageNames = []string{"artist", "folder", "artist-poster"}

// MusicArtwork gives albums a cover (a picture file in the album folder, else the
// art embedded in a track) and artists a picture (from their folder, else the
// cover of one of their albums).
func (r *Refresher) MusicArtwork(ctx context.Context, libraryID uuid.UUID) (int, error) {
	if r.ImageDir == "" || r.ResolvePath == nil {
		return 0, nil
	}
	rows, err := r.Pool.Query(ctx, `SELECT a.id, a.has_poster, f.path FROM items a
		JOIN LATERAL (SELECT m.path FROM items t JOIN media_files m ON m.item_id = t.id
			WHERE t.parent_id = a.id ORDER BY t.season_number NULLS FIRST, t.episode_number NULLS LAST LIMIT 1) f ON true
		WHERE a.library_id = $1 AND a.kind = 'album'`, libraryID)
	if err != nil {
		return 0, err
	}
	type album struct {
		id        uuid.UUID
		hasPoster bool
		track     string
	}
	var albums []album
	for rows.Next() {
		var a album
		if err := rows.Scan(&a.id, &a.hasPoster, &a.track); err != nil {
			rows.Close()
			return 0, err
		}
		albums = append(albums, a)
	}
	rows.Close()
	done := 0
	for _, a := range albums {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		host, err := r.ResolvePath(a.track)
		if err != nil {
			continue
		}
		dst := ImagePath(r.ImageDir, a.id, "poster")
		marker := LocalMarker(dst)
		dir := filepath.Dir(host)
		src := findLocal(dir, "", albumCoverNames)
		if src == "" {
			src = findLocal(filepath.Dir(dir), "", albumCoverNames) // Album/CD1/…
		}
		switch {
		case src != "" && newer(src, marker):
			if !r.importImage(ctx, src, dst, 1000, false) || os.WriteFile(marker, nil, 0o600) != nil {
				continue
			}
		case src == "" && !a.hasPoster:
			if !r.embeddedCover(ctx, host, dst) {
				continue
			}
		default:
			continue
		}
		if _, err := r.Pool.Exec(ctx, `UPDATE items SET has_poster = true, colors = NULL WHERE id = $1`, a.id); err != nil {
			return done, err
		}
		done++
	}

	// Artists: their own picture, else the cover of their newest album.
	rows, err = r.Pool.Query(ctx, `SELECT ar.id, ar.has_poster,
			(SELECT al.id FROM items al WHERE al.parent_id = ar.id AND al.has_poster ORDER BY al.year DESC NULLS LAST LIMIT 1),
			(SELECT m.path FROM items al JOIN items t ON t.parent_id = al.id JOIN media_files m ON m.item_id = t.id
				WHERE al.parent_id = ar.id LIMIT 1)
		FROM items ar WHERE ar.library_id = $1 AND ar.kind = 'artist'`, libraryID)
	if err != nil {
		return done, err
	}
	type artist struct {
		id        uuid.UUID
		hasPoster bool
		album     *uuid.UUID
		track     *string
	}
	var artists []artist
	for rows.Next() {
		var a artist
		if err := rows.Scan(&a.id, &a.hasPoster, &a.album, &a.track); err != nil {
			rows.Close()
			return done, err
		}
		artists = append(artists, a)
	}
	rows.Close()
	for _, a := range artists {
		dst := ImagePath(r.ImageDir, a.id, "poster")
		var src string
		if a.track != nil {
			if host, err := r.ResolvePath(*a.track); err == nil {
				// Artist/Album/track: the artist folder is two levels up.
				src = findLocal(filepath.Dir(filepath.Dir(host)), "", artistImageNames)
			}
		}
		switch {
		case src != "" && newer(src, LocalMarker(dst)):
			if !r.importImage(ctx, src, dst, 1000, false) || os.WriteFile(LocalMarker(dst), nil, 0o600) != nil {
				continue
			}
		case src == "" && !a.hasPoster && a.album != nil:
			data, err := os.ReadFile(ImagePath(r.ImageDir, *a.album, "poster")) //nolint:gosec // G304: cache path built from a UUID
			if err != nil || os.MkdirAll(filepath.Dir(dst), 0o750) != nil || os.WriteFile(dst, data, 0o600) != nil {
				continue
			}
		default:
			continue
		}
		if _, err := r.Pool.Exec(ctx, `UPDATE items SET has_poster = true, colors = NULL WHERE id = $1`, a.id); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

// embeddedCover extracts the cover art stored inside an audio file.
func (r *Refresher) embeddedCover(ctx context.Context, src, dst string) bool {
	if r.FFmpeg == "" || os.MkdirAll(filepath.Dir(dst), 0o750) != nil {
		return false
	}
	tmp := dst + ".tmp.jpg"
	cctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, r.FFmpeg, "-nostdin", "-v", "error", "-y", "-i", src, "-an", "-map", "0:v:0", //nolint:gosec // G204: fixed flags, path from ResolvePath
		"-frames:v", "1", "-vf", fmt.Sprintf("scale='min(%d,iw)':-2", 1000), "-q:v", "3", tmp)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		return false
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() < 512 {
		_ = os.Remove(tmp)
		return false
	}
	return os.Rename(tmp, dst) == nil
}
