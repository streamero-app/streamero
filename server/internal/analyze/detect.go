package analyze

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"math"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Detector finds the intro and the end credits of episodes by comparing the
// audio of the episodes of a season (Chromaprint fingerprints); credits on a
// black background are found from the picture when the audio finds none.
// It is incremental: fingerprints are stored and only new or replaced files
// are decoded again (D-035).
type Detector struct {
	Pool   *pgxpool.Pool
	FFmpeg string
	// Fpcalc is Chromaprint's command-line tool; when it is missing, ffmpeg's
	// chromaprint muxer computes the same values.
	Fpcalc      string
	ResolvePath func(path string) (string, error)
	Log         *slog.Logger
	// IntroSeconds and CreditsSeconds are the windows searched at the start and at
	// the end of each episode (defaults 10 and 5 minutes; at most 30 % / 25 % of the episode).
	IntroSeconds, CreditsSeconds float64
}

type episodeRow struct {
	id         uuid.UUID
	path       string
	durationMS int64
	size       int64
	mtime      time.Time
	fresh      bool
	prints     Prints
	black      *int64 // nil = not checked; -1 = none
}

// Segment sources by precedence: a source never replaces a stronger one.
const precedence = `CASE %s WHEN 'manual' THEN 4 WHEN 'chapter' THEN 3 WHEN 'fingerprint' THEN 2 ELSE 1 END`

// SaveSegment stores s unless a stronger source already describes that kind.
func SaveSegment(ctx context.Context, pool *pgxpool.Pool, fileID uuid.UUID, s Segment) error {
	_, err := pool.Exec(ctx, `INSERT INTO media_segments (file_id, kind, start_ms, end_ms, source) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (file_id, kind) DO UPDATE SET start_ms = EXCLUDED.start_ms, end_ms = EXCLUDED.end_ms, source = EXCLUDED.source
		WHERE `+fmt.Sprintf(precedence, "media_segments.source")+` <= `+fmt.Sprintf(precedence, "EXCLUDED.source"),
		fileID, s.Kind, s.StartMS, s.EndMS, s.Source)
	return err
}

// Run detects intros and credits in the seasons of a library that have new or
// changed episodes; it returns how many episodes were fingerprinted.
func (d *Detector) Run(ctx context.Context, libraryID uuid.UUID) (int, error) {
	// Seasons with at least one episode whose fingerprints are missing or stale.
	rows, err := d.Pool.Query(ctx, `SELECT f.id, f.path, f.duration_ms, f.size, f.mtime, i.parent_id,
			(fp.file_id IS NOT NULL AND fp.size = f.size AND fp.mtime = f.mtime),
			fp.intro, fp.credits, COALESCE(fp.credits_offset_ms, 0), fp.black_credits_ms
		FROM media_files f JOIN items i ON i.id = f.item_id
		LEFT JOIN media_fingerprints fp ON fp.file_id = f.id
		WHERE f.library_id = $1 AND i.kind = 'episode' AND i.parent_id IS NOT NULL AND COALESCE(f.duration_ms, 0) > 0
			AND i.parent_id IN (
				SELECT i2.parent_id FROM media_files f2 JOIN items i2 ON i2.id = f2.item_id
				LEFT JOIN media_fingerprints fp2 ON fp2.file_id = f2.id
				WHERE f2.library_id = $1 AND i2.kind = 'episode' AND COALESCE(f2.duration_ms, 0) > 0
					AND (fp2.file_id IS NULL OR fp2.size <> f2.size OR fp2.mtime <> f2.mtime))
		ORDER BY i.parent_id, i.season_number NULLS LAST, i.episode_number NULLS LAST, f.path`, libraryID)
	if err != nil {
		return 0, err
	}
	seasons := map[uuid.UUID][]*episodeRow{}
	var order []uuid.UUID
	for rows.Next() {
		e := &episodeRow{}
		var season uuid.UUID
		var intro, credits []byte
		if err := rows.Scan(&e.id, &e.path, &e.durationMS, &e.size, &e.mtime, &season, &e.fresh, &intro, &credits,
			&e.prints.CreditsOffsetMS, &e.black); err != nil {
			rows.Close()
			return 0, err
		}
		e.prints.Intro, e.prints.Credits, e.prints.DurationMS = decodePrint(intro), decodePrint(credits), e.durationMS
		if _, ok := seasons[season]; !ok {
			order = append(order, season)
		}
		seasons[season] = append(seasons[season], e)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return 0, err
	}
	done := 0
	for _, s := range order {
		n, err := d.season(ctx, seasons[s])
		done += n
		if err != nil {
			return done, err
		}
	}
	return done, nil
}

func (d *Detector) windows(durationMS int64) (introSec, creditsSec float64) {
	introSec, creditsSec = d.IntroSeconds, d.CreditsSeconds
	if introSec <= 0 {
		introSec = 600
	}
	if creditsSec <= 0 {
		creditsSec = 300
	}
	total := float64(durationMS) / 1000
	return min(introSec, total*0.3), min(creditsSec, total*0.25)
}

func (d *Detector) season(ctx context.Context, eps []*episodeRow) (int, error) {
	done := 0
	for _, e := range eps {
		if e.fresh {
			continue
		}
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		src, ok := d.resolve(e.path)
		introSec, creditsSec := d.windows(e.durationMS)
		e.prints.Intro, e.prints.Credits, e.black = nil, nil, nil
		e.prints.CreditsOffsetMS = e.durationMS - secMS(creditsSec)
		if ok {
			e.prints.Intro = d.fingerprint(ctx, src, 0, introSec)
			e.prints.Credits = d.fingerprint(ctx, src, float64(e.prints.CreditsOffsetMS)/1000, creditsSec)
		}
		if ctx.Err() != nil {
			return done, ctx.Err()
		}
		if _, err := d.Pool.Exec(ctx, `INSERT INTO media_fingerprints (file_id, size, mtime, intro, credits, intro_ms, credits_offset_ms, black_credits_ms)
			VALUES ($1, $2, $3, $4, $5, $6, $7, NULL)
			ON CONFLICT (file_id) DO UPDATE SET size = EXCLUDED.size, mtime = EXCLUDED.mtime, intro = EXCLUDED.intro,
				credits = EXCLUDED.credits, intro_ms = EXCLUDED.intro_ms, credits_offset_ms = EXCLUDED.credits_offset_ms,
				black_credits_ms = NULL, created_at = now()`,
			e.id, e.size, e.mtime, encodePrint(e.prints.Intro), encodePrint(e.prints.Credits), secMS(introSec), e.prints.CreditsOffsetMS); err != nil {
			return done, err
		}
		done++
	}

	// Segments that a stronger source (chapter names, manual edits) already gives.
	ids := make([]uuid.UUID, len(eps))
	for i, e := range eps {
		ids[i] = e.id
	}
	strong := map[uuid.UUID]map[string]bool{}
	rows, err := d.Pool.Query(ctx, `SELECT file_id, kind FROM media_segments WHERE file_id = ANY($1) AND source IN ('manual', 'chapter')`, ids)
	if err != nil {
		return done, err
	}
	for rows.Next() {
		var id uuid.UUID
		var kind string
		if err := rows.Scan(&id, &kind); err != nil {
			rows.Close()
			return done, err
		}
		if strong[id] == nil {
			strong[id] = map[string]bool{}
		}
		strong[id][kind] = true
	}
	rows.Close()

	prints := make([]Prints, len(eps))
	for i, e := range eps {
		prints[i] = e.prints
	}
	found := MatchSeason(prints)
	for i, e := range eps {
		if err := d.store(ctx, e.id, "intro", found[i].Intro, "fingerprint"); err != nil {
			return done, err
		}
		credits, source := found[i].Credits, "fingerprint"
		if credits == nil && !strong[e.id]["credits"] {
			if c, ok := d.blackCredits(ctx, e); ok {
				credits, source = &c, "blackframe"
			}
		}
		if err := d.store(ctx, e.id, "credits", credits, source); err != nil {
			return done, err
		}
	}
	return done, nil
}

// store saves a detected segment, or removes an earlier detection that no longer holds.
func (d *Detector) store(ctx context.Context, fileID uuid.UUID, kind string, s *Span, source string) error {
	if s == nil || s.EndMS <= s.StartMS {
		_, err := d.Pool.Exec(ctx, `DELETE FROM media_segments WHERE file_id = $1 AND kind = $2 AND source IN ('fingerprint', 'blackframe')`, fileID, kind)
		return err
	}
	if source == "blackframe" {
		// A weaker source must replace an outdated fingerprint match.
		if _, err := d.Pool.Exec(ctx, `DELETE FROM media_segments WHERE file_id = $1 AND kind = $2 AND source = 'fingerprint'`, fileID, kind); err != nil {
			return err
		}
	}
	return SaveSegment(ctx, d.Pool, fileID, Segment{Kind: kind, StartMS: s.StartMS, EndMS: s.EndMS, Source: source})
}

// blackCredits runs (once per file version) black-frame detection over the end of an episode.
func (d *Detector) blackCredits(ctx context.Context, e *episodeRow) (Span, bool) {
	if e.black == nil {
		v := int64(-1)
		if src, ok := d.resolve(e.path); ok && d.FFmpeg != "" {
			_, creditsSec := d.windows(e.durationMS)
			offset := e.durationMS - secMS(creditsSec)
			if c, ok := CreditsFromBlack(d.blackdetect(ctx, src, offset, creditsSec), e.durationMS); ok {
				v = c.StartMS
			}
		}
		if ctx.Err() != nil {
			return Span{}, false
		}
		e.black = &v
		_, _ = d.Pool.Exec(ctx, `UPDATE media_fingerprints SET black_credits_ms = $2 WHERE file_id = $1`, e.id, v)
	}
	if *e.black < 0 || *e.black >= e.durationMS {
		return Span{}, false
	}
	return Span{*e.black, e.durationMS}, true
}

func (d *Detector) blackdetect(ctx context.Context, src string, offsetMS int64, seconds float64) []Span {
	cctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	// Mostly-black pictures (credits text covers a few per cent of the frame).
	// #nosec G204 -- fixed binary and flags; the path was validated by ResolvePath.
	cmd := exec.CommandContext(cctx, d.FFmpeg, "-nostdin", "-hide_banner", "-loglevel", "info",
		"-ss", fmt.Sprintf("%.3f", float64(offsetMS)/1000), "-t", fmt.Sprintf("%.3f", seconds), "-i", src,
		"-an", "-sn", "-dn", "-vf", "scale=320:-2,blackdetect=d=1:pic_th=0.90:pix_th=0.10", "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if d.Log != nil {
			d.Log.Debug("blackdetect failed", "err", err)
		}
		return nil
	}
	return ParseBlackDetect(stderr.String(), offsetMS)
}

func (d *Detector) resolve(path string) (string, bool) {
	if d.ResolvePath == nil {
		return "", false
	}
	p, err := d.ResolvePath(path)
	return p, err == nil
}

// fingerprint returns the Chromaprint values of [start, start+dur) seconds of audio,
// from fpcalc when it is installed, otherwise from ffmpeg's chromaprint muxer.
func (d *Detector) fingerprint(ctx context.Context, src string, start, dur float64) []uint32 {
	if d.FFmpeg == "" || dur <= 0 {
		return nil
	}
	if fp, err := exec.LookPath(d.Fpcalc); d.Fpcalc != "" && err == nil {
		return fpcalcPrint(ctx, d.FFmpeg, fp, src, start, dur)
	}
	return muxerPrint(ctx, d.FFmpeg, src, start, dur)
}

// fpcalcPrint decodes the passage with ffmpeg (fpcalc cannot seek) and pipes raw
// 11025 Hz mono samples into fpcalc.
func fpcalcPrint(ctx context.Context, ffmpeg, fpcalc, src string, start, dur float64) []uint32 {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// #nosec G204 -- fixed binary and flags; the path was validated by ResolvePath.
	dec := exec.CommandContext(cctx, ffmpeg, "-nostdin", "-v", "error", "-ss", fmt.Sprintf("%.3f", start), "-t", fmt.Sprintf("%.3f", dur),
		"-i", src, "-vn", "-sn", "-dn", "-ac", "1", "-ar", "11025", "-f", "s16le", "-")
	// #nosec G204 -- fixed binary and flags.
	fpc := exec.CommandContext(cctx, fpcalc, "-format", "s16le", "-rate", "11025", "-channels", "1", "-raw",
		"-length", strconv.Itoa(int(math.Ceil(dur))+1), "-")
	pipe, err := dec.StdoutPipe()
	if err != nil {
		return nil
	}
	fpc.Stdin = pipe
	var out bytes.Buffer
	fpc.Stdout = &out
	if err := dec.Start(); err != nil {
		return nil
	}
	if err := fpc.Run(); err != nil {
		_ = dec.Process.Kill()
		_ = dec.Wait()
		return nil
	}
	if err := dec.Wait(); err != nil {
		return nil
	}
	return parseFpcalc(out.String())
}

// parseFpcalc reads the FINGERPRINT= line of `fpcalc -raw`.
func parseFpcalc(out string) []uint32 {
	for _, line := range strings.Split(out, "\n") {
		v, ok := strings.CutPrefix(strings.TrimSpace(line), "FINGERPRINT=")
		if !ok {
			continue
		}
		var fp []uint32
		for _, f := range strings.Split(v, ",") {
			n, err := strconv.ParseInt(strings.TrimSpace(f), 10, 64)
			if err != nil {
				return nil
			}
			fp = append(fp, uint32(n)) // #nosec G115 -- -signed output wraps the same 32 bits
		}
		return fp
	}
	return nil
}

func muxerPrint(ctx context.Context, ffmpeg, src string, start, dur float64) []uint32 {
	cctx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()
	// #nosec G204 -- fixed binary and flags; the path was validated by ResolvePath.
	out, err := exec.CommandContext(cctx, ffmpeg, "-nostdin", "-v", "error", "-ss", fmt.Sprintf("%.2f", start), "-t", fmt.Sprintf("%.2f", dur),
		"-i", src, "-vn", "-sn", "-ac", "1", "-f", "chromaprint", "-fp_format", "raw", "-").Output()
	if err != nil || len(out) < 4 {
		return nil
	}
	return decodePrint(out)
}

func decodePrint(b []byte) []uint32 {
	fp := make([]uint32, len(b)/4)
	for i := range fp {
		fp[i] = binary.LittleEndian.Uint32(b[i*4:])
	}
	return fp
}

func encodePrint(fp []uint32) []byte {
	if len(fp) == 0 {
		return nil
	}
	b := make([]byte, len(fp)*4)
	for i, v := range fp {
		binary.LittleEndian.PutUint32(b[i*4:], v)
	}
	return b
}
