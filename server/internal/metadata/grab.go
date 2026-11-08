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

// GrabFrames gives movies and episodes without artwork a frame from their own
// video, so nothing in the library shows an empty card. Each item is tried once
// (art_grabbed_at); TMDb artwork found later simply replaces the frame.
func (r *Refresher) GrabFrames(ctx context.Context, libraryID uuid.UUID) (int, error) {
	if r.FFmpeg == "" || r.ImageDir == "" || r.ResolvePath == nil {
		return 0, nil
	}
	rows, err := r.Pool.Query(ctx, `SELECT i.id, i.kind, f.path, COALESCE(f.duration_ms, 0), COALESCE(f.hdr, '')
		FROM items i
		JOIN LATERAL (SELECT path, duration_ms, hdr FROM media_files m WHERE m.item_id = i.id
			ORDER BY m.height DESC NULLS LAST LIMIT 1) f ON true
		WHERE i.library_id = $1 AND i.art_grabbed_at IS NULL
			AND ((i.kind = 'movie' AND NOT i.has_backdrop) OR (i.kind = 'episode' AND NOT i.has_poster))
		ORDER BY i.added_at DESC LIMIT 500`, libraryID)
	if err != nil {
		return 0, err
	}
	type job struct {
		id         uuid.UUID
		kind, path string
		durMS      int64
		hdr        string
	}
	var jobs []job
	for rows.Next() {
		var j job
		if err := rows.Scan(&j.id, &j.kind, &j.path, &j.durMS, &j.hdr); err != nil {
			rows.Close()
			return 0, err
		}
		jobs = append(jobs, j)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	done := 0
	for _, j := range jobs {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		kind, column := "backdrop", "has_backdrop"
		if j.kind == "episode" {
			kind, column = "still", "has_poster"
		}
		ok := r.grab(ctx, j.path, j.durMS, j.hdr != "", ImagePath(r.ImageDir, j.id, kind))
		// column is one of two constants above, never input.
		q := fmt.Sprintf(`UPDATE items SET art_grabbed_at = now(), %s = (%s OR $2) WHERE id = $1`, column, column)
		if _, err := r.Pool.Exec(ctx, q, j.id, ok); err != nil {
			return done, err
		}
		if ok {
			done++
		}
	}
	return done, nil
}

// grab writes one frame (15% in, past any cold open) as a 1280-wide JPEG.
func (r *Refresher) grab(ctx context.Context, path string, durMS int64, hdr bool, dst string) bool {
	src, err := r.ResolvePath(path)
	if err != nil {
		return false
	}
	at := float64(durMS) / 1000 * 0.15
	if at > 600 {
		at = 600
	}
	vf := "scale=1280:-2"
	if hdr {
		vf = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p," + vf
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o750); err != nil {
		return false
	}
	tmp := dst + ".tmp.jpg"
	cctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	// Arguments are fixed flags plus a resolved library path; no shell is involved.
	cmd := exec.CommandContext(cctx, r.FFmpeg, "-nostdin", "-v", "error", "-y", //nolint:gosec // G204: path checked by ResolvePath
		"-ss", fmt.Sprintf("%.2f", at), "-i", src, "-frames:v", "1", "-vf", vf, "-q:v", "3", tmp)
	if err := cmd.Run(); err != nil {
		_ = os.Remove(tmp)
		r.Log.Debug("frame grab failed", "path", path, "err", err)
		return false
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() < 1024 {
		_ = os.Remove(tmp)
		return false
	}
	return os.Rename(tmp, dst) == nil
}
