package analyze

import (
	"context"
	"encoding/binary"
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
	id               uuid.UUID
	path             string
	durationMS       int64
	width, height    int
	hdr, kind        string
	season           *uuid.UUID
	episode          *int
	analyzed         bool
	chapterSegments  map[string]bool
	introFP, endFP   []uint32
	endOffsetSeconds float64
}

// Run analyzes every file of the library that has not been analyzed yet.
// Episodes are compared with the other episodes of their season to find a shared
// intro and end credits.
func (a *Analyzer) Run(ctx context.Context, libraryID uuid.UUID) (int, error) {
	rows, err := a.Pool.Query(ctx, `SELECT f.id, f.path, COALESCE(f.duration_ms, 0), COALESCE(f.width, 0), COALESCE(f.height, 0),
			f.hdr, i.kind, CASE WHEN i.kind = 'episode' THEN i.parent_id END, i.episode_number, f.analyzed_at IS NOT NULL
		FROM media_files f JOIN items i ON i.id = f.item_id
		WHERE f.library_id = $1 AND COALESCE(f.duration_ms, 0) > 0 AND i.kind IN ('movie', 'episode')
			AND (f.analyzed_at IS NULL OR (i.kind = 'episode' AND i.parent_id IN (
				SELECT i2.parent_id FROM media_files f2 JOIN items i2 ON i2.id = f2.item_id
				WHERE f2.library_id = $1 AND f2.analyzed_at IS NULL AND i2.kind = 'episode')))
		ORDER BY i.parent_id, i.episode_number`, libraryID)
	if err != nil {
		return 0, err
	}
	var files []*file
	for rows.Next() {
		f := &file{chapterSegments: map[string]bool{}}
		if err := rows.Scan(&f.id, &f.path, &f.durationMS, &f.width, &f.height, &f.hdr, &f.kind, &f.season, &f.episode, &f.analyzed); err != nil {
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
		if f.analyzed {
			continue
		}
		if ctx.Err() != nil {
			return 0, ctx.Err()
		}
		chs := a.chapters(ctx, f.path)
		raw, _ := json.Marshal(chs)
		if _, err := a.Pool.Exec(ctx, `UPDATE media_files SET chapters = $2 WHERE id = $1`, f.id, raw); err != nil {
			return 0, err
		}
		if _, err := a.Pool.Exec(ctx, `DELETE FROM media_segments WHERE file_id = $1 AND source <> 'manual'`, f.id); err != nil {
			return 0, err
		}
		for _, s := range SegmentsFromChapters(chs, f.durationMS) {
			f.chapterSegments[s.Kind] = true
			if err := a.saveSegment(ctx, f.id, s); err != nil {
				return 0, err
			}
		}
	}

	// 2. Intro and credits shared between episodes of a season.
	seasons := map[uuid.UUID][]*file{}
	for _, f := range files {
		if f.season != nil {
			seasons[*f.season] = append(seasons[*f.season], f)
		}
	}
	for _, eps := range seasons {
		if len(eps) < 2 {
			continue
		}
		for _, f := range eps {
			if ctx.Err() != nil {
				return 0, ctx.Err()
			}
			a.fingerprints(ctx, f)
		}
		for i, f := range eps {
			if f.analyzed {
				continue
			}
			other := eps[(i+1)%len(eps)]
			if i == len(eps)-1 {
				other = eps[i-1]
			}
			if !f.chapterSegments["intro"] {
				if m, ok := Longest(f.introFP, other.introFP, 15, 150); ok {
					_ = a.saveSegment(ctx, f.id, Segment{Kind: "intro", StartMS: secMS(float64(m.AStart) * ItemSeconds),
						EndMS: secMS(float64(m.AEnd) * ItemSeconds), Source: "fingerprint"})
				}
			}
			if !f.chapterSegments["credits"] {
				if m, ok := Longest(f.endFP, other.endFP, 15, 300); ok {
					start := secMS(f.endOffsetSeconds + float64(m.AStart)*ItemSeconds)
					end := secMS(f.endOffsetSeconds + float64(m.AEnd)*ItemSeconds)
					if f.durationMS-end < 10_000 {
						end = f.durationMS // credits run to the end
					}
					_ = a.saveSegment(ctx, f.id, Segment{Kind: "credits", StartMS: start, EndMS: end, Source: "fingerprint"})
				}
			}
		}
	}

	// 3. Trickplay, then mark done.
	done := 0
	for _, f := range files {
		if f.analyzed {
			continue
		}
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

func (a *Analyzer) saveSegment(ctx context.Context, fileID uuid.UUID, s Segment) error {
	_, err := a.Pool.Exec(ctx, `INSERT INTO media_segments (file_id, kind, start_ms, end_ms, source) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (file_id, kind) DO UPDATE SET start_ms = EXCLUDED.start_ms, end_ms = EXCLUDED.end_ms, source = EXCLUDED.source
		WHERE media_segments.source <> 'manual'`, fileID, s.Kind, s.StartMS, s.EndMS, s.Source)
	return err
}

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

// fingerprint returns Chromaprint values for [start, start+dur) seconds of audio.
func (a *Analyzer) fingerprint(ctx context.Context, src string, start, dur float64) []uint32 {
	cctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	// #nosec G204 -- fixed binary and flags; the path was validated by ResolvePath.
	out, err := exec.CommandContext(cctx, a.FFmpeg, "-nostdin", "-v", "error", "-ss", fmt.Sprintf("%.2f", start), "-t", fmt.Sprintf("%.2f", dur),
		"-i", src, "-vn", "-sn", "-ac", "1", "-f", "chromaprint", "-fp_format", "raw", "-").Output()
	if err != nil || len(out) < 4 {
		return nil
	}
	fp := make([]uint32, len(out)/4)
	for i := range fp {
		fp[i] = binary.LittleEndian.Uint32(out[i*4:])
	}
	return fp
}

func (a *Analyzer) fingerprints(ctx context.Context, f *file) {
	src, ok := a.resolve(f.path)
	if !ok || a.FFmpeg == "" {
		return
	}
	total := float64(f.durationMS) / 1000
	f.introFP = a.fingerprint(ctx, src, 0, min(600, total*0.3))
	endLen := min(300, total*0.25)
	f.endOffsetSeconds = total - endLen
	f.endFP = a.fingerprint(ctx, src, f.endOffsetSeconds, endLen)
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
