package analyze

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Trickplay describes the thumbnail sheets of a file.
type Trickplay struct {
	IntervalMS int `json:"intervalMs"`
	Width      int `json:"width"`
	Height     int `json:"height"`
	Columns    int `json:"columns"`
	Rows       int `json:"rows"`
	Count      int `json:"count"`  // thumbnails
	Sheets     int `json:"sheets"` // images
}

const (
	trickInterval = 10_000
	trickWidth    = 320
	trickCols     = 10
	trickRows     = 10
)

// TrickplayDir is where the sheets of a file live (sheet N is N.jpg, from 0).
func TrickplayDir(cache string, fileID uuid.UUID) string {
	return filepath.Join(cache, "trickplay", fileID.String())
}

// Analyzer runs ffprobe/ffmpeg over a library's files once each.
type Analyzer struct {
	Pool        *pgxpool.Pool
	FFmpeg      string
	FFprobe     string
	CacheDir    string
	Trickplay   bool
	ResolvePath func(path string) (string, error)
	Log         *slog.Logger
}

type file struct {
	id            uuid.UUID
	path          string
	durationMS    int64
	width, height int
	hdr, kind     string
}

// Run analyzes every file of the library that has not been analyzed yet:
// chapters (and segments named by them) and trickplay. Intro and credits found
// by comparing the episodes of a season are the Detector's job.
func (a *Analyzer) Run(ctx context.Context, libraryID uuid.UUID) (int, error) {
	rows, err := a.Pool.Query(ctx, `SELECT f.id, f.path, COALESCE(f.duration_ms, 0), COALESCE(f.width, 0), COALESCE(f.height, 0),
			f.hdr, i.kind
		FROM media_files f JOIN items i ON i.id = f.item_id
		WHERE f.library_id = $1 AND COALESCE(f.duration_ms, 0) > 0 AND i.kind IN ('movie', 'episode') AND f.analyzed_at IS NULL
		ORDER BY i.parent_id, i.episode_number`, libraryID)
	if err != nil {
		return 0, err
	}
	var files []*file
	for rows.Next() {
		f := &file{}
		if err := rows.Scan(&f.id, &f.path, &f.durationMS, &f.width, &f.height, &f.hdr, &f.kind); err != nil {
			rows.Close()
			return 0, err
		}
		files = append(files, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}

	// 1. Chapters and chapter-named segments.
	for _, f := range files {
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		chs := a.chapters(ctx, f.path)
		raw, _ := json.Marshal(chs)
		if _, err := a.Pool.Exec(ctx, `UPDATE media_files SET chapters = $2 WHERE id = $1`, f.id, raw); err != nil {
			return 0, err
		}
		if _, err := a.Pool.Exec(ctx, `DELETE FROM media_segments WHERE file_id = $1 AND source = 'chapter'`, f.id); err != nil {
			return 0, err
		}
		for _, s := range SegmentsFromChapters(chs, f.durationMS) {
			if err := SaveSegment(ctx, a.Pool, f.id, s); err != nil {
				return 0, err
			}
		}
	}

	// 2. Trickplay, then mark done.
	done := 0
	for _, f := range files {
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		var tp *Trickplay
		if a.Trickplay && (f.kind == "movie" || f.kind == "episode") {
			tp = a.trickplay(ctx, f)
		}
		var raw []byte
		if tp != nil {
			raw, _ = json.Marshal(tp)
		}
		if _, err := a.Pool.Exec(ctx, `UPDATE media_files SET trickplay = $2, analyzed_at = now() WHERE id = $1`, f.id, raw); err != nil {
			return done, err
		}
		done++
	}
	return done, nil
}

func secMS(s float64) int64 { return int64(s * 1000) }

func (a *Analyzer) resolve(path string) (string, bool) {
	if a.ResolvePath == nil {
		return "", false
	}
	p, err := a.ResolvePath(path)
	return p, err == nil
}

func (a *Analyzer) chapters(ctx context.Context, path string) []Chapter {
	src, ok := a.resolve(path)
	if !ok || a.FFprobe == "" {
		return []Chapter{}
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	// #nosec G204 -- fixed binary; the path was validated by ResolvePath and is one argument.
	out, err := exec.CommandContext(cctx, a.FFprobe, "-v", "error", "-print_format", "json", "-show_chapters", "--", src).Output()
	if err != nil {
		return []Chapter{}
	}
	var o struct {
		Chapters []struct {
			StartTime string            `json:"start_time"`
			Tags      map[string]string `json:"tags"`
		} `json:"chapters"`
	}
	if json.Unmarshal(out, &o) != nil {
		return []Chapter{}
	}
	chs := make([]Chapter, 0, len(o.Chapters))
	for _, c := range o.Chapters {
		st, _ := strconv.ParseFloat(c.StartTime, 64)
		chs = append(chs, Chapter{StartMS: secMS(st), Title: c.Tags["title"]})
	}
	return chs
}

func (a *Analyzer) trickplay(ctx context.Context, f *file) *Trickplay {
	src, ok := a.resolve(f.path)
	if !ok || a.FFmpeg == "" || a.CacheDir == "" || f.width <= 0 || f.height <= 0 {
		return nil
	}
	h := (trickWidth*f.height/f.width + 1) &^ 1
	dir := TrickplayDir(a.CacheDir, f.id)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return nil
	}
	vf := fmt.Sprintf("fps=1000/%d,scale=%d:%d,tile=%dx%d", trickInterval, trickWidth, h, trickCols, trickRows)
	if f.hdr != "" {
		vf = "zscale=t=linear:npl=100,format=gbrpf32le,zscale=p=bt709,tonemap=tonemap=hable:desat=0,zscale=t=bt709:m=bt709:r=tv,format=yuv420p," + vf
	}
	cctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	// Keyframes only: a full decode is not needed for 1-in-10-seconds thumbnails.
	// #nosec G204 -- fixed binary and flags; the path was validated by ResolvePath.
	cmd := exec.CommandContext(cctx, a.FFmpeg, "-nostdin", "-v", "error", "-skip_frame", "nokey", "-i", src,
		"-an", "-sn", "-vf", vf, "-fps_mode", "vfr", "-q:v", "5", "-start_number", "0", filepath.Join(dir, "%d.jpg"))
	if out, err := cmd.CombinedOutput(); err != nil {
		if a.Log != nil {
			a.Log.Warn("trickplay failed", "path", f.path, "err", err, "out", string(out))
		}
		_ = os.RemoveAll(dir)
		return nil
	}
	sheets, _ := filepath.Glob(filepath.Join(dir, "*.jpg"))
	if len(sheets) == 0 {
		return nil
	}
	count := int(f.durationMS/trickInterval) + 1
	return &Trickplay{IntervalMS: trickInterval, Width: trickWidth, Height: h, Columns: trickCols, Rows: trickRows,
		Count: min(count, len(sheets)*trickCols*trickRows), Sheets: len(sheets)}
}
