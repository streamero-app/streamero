package dlna

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/metadata"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// signed checks the URL signature for scope "/dlna/<kind>/<id>/".
func (s *Server) signed(r *http.Request, kind, id string) bool {
	return s.Stream.Signer.Valid("/dlna/"+kind+"/"+id+"/", r.URL.Query())
}

// fileRow is a media file with its item.
type fileRow struct {
	library.MediaFile
	ItemID uuid.UUID
}

func (s *Server) fileRow(ctx context.Context, id uuid.UUID) (fileRow, error) {
	var f fileRow
	var audio, subs []byte
	err := s.Pool.QueryRow(ctx, `SELECT item_id, id, path, size, container, duration_ms, width, height, video_codec, hdr, audio, subtitles
		FROM media_files WHERE id = $1`, id).Scan(&f.ItemID, &f.ID, &f.Path, &f.Size, &f.Container, &f.DurationMS, &f.Width,
		&f.Height, &f.VideoCodec, &f.HDR, &audio, &subs)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, library.ErrNotFound
	}
	_ = json.Unmarshal(audio, &f.Audio)
	_ = json.Unmarshal(subs, &f.Subtitles)
	return f, err
}

// setExact sets a header with exactly this spelling: Go would turn
// "contentFeatures.dlna.org" into "Contentfeatures.dlna.org", and some
// renderers compare DLNA header names case-sensitively.
func setExact(h http.Header, k, v string) {
	delete(h, http.CanonicalHeaderKey(k))
	h[k] = []string{v}
}

// mediaHeaders sets the DLNA response headers common to media responses;
// captions offers the Samsung subtitle header (only for streams starting at zero).
func (s *Server) mediaHeaders(w http.ResponseWriter, r *http.Request, mime, features string, f fileRow, captions bool) {
	h := w.Header()
	h.Set("Content-Type", mime)
	setExact(h, transferHeader, transferStream)
	setExact(h, featuresHeader, features) // asked for with getcontentFeatures.dlna.org; harmless otherwise
	if captions && r.Header.Get("getCaptionInfo.sec") == "1" && f.VideoCodec != "" {
		_, v := s.Current(r.Context())
		if v != nil {
			if i := pickSubtitle(f.MediaFile, 0, v.Prefs); i >= 0 {
				scope := "/dlna/s/" + f.ID.String() + "/"
				setExact(h, captionHeader, s.baseFor(r)+scope+strconv.Itoa(i)+".srt?"+s.Stream.Signer.Query(scope, stream.URLTTL))
			}
		}
	}
}

// direct serves a file as-is with byte ranges (DLNA.ORG_OP=01).
func (s *Server) direct(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("fileId"))
	if err != nil || !s.signed(r, "f", id.String()) {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return
	}
	if r.Header.Get(timeSeekHeader) != "" {
		// Time seeking is offered only on converted streams; the file seeks by bytes.
		http.Error(w, "time seek not supported on this resource", http.StatusNotAcceptable)
		return
	}
	row, err := s.fileRow(r.Context(), id)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	p, err := s.Stream.FilePath(r.Context(), id) // re-validated with library.ResolveSafePath
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
	prof := Detect(r.Header)
	audioOnly := row.VideoCodec == ""
	mime := prof.MIMEType(row.Container, audioOnly)
	pn := ""
	if audioOnly {
		pn = audioPN(mime)
	}
	s.mediaHeaders(w, r, mime, Features(pn, true, false), row, true)
	var dur int64
	if row.DurationMS != nil {
		dur = *row.DurationMS
	}
	cw := &countingWriter{ResponseWriter: w}
	if r.Method == http.MethodGet && st.Size() > 0 && dur > 0 {
		offset := rangeStart(r.Header.Get("Range"))
		size := st.Size()
		play := s.tracker.start(s, r, row.ItemID, dur, prof.Label, "direct")
		cw.onWrite = func(n int64) {
			// Small reads (container index at the end of a file) are not playback.
			if n >= 1<<20 {
				play.at(min(dur, (offset+n)*dur/size))
			}
		}
	}
	http.ServeContent(cw, r, "", st.ModTime(), f)
}

func rangeStart(h string) int64 {
	v, ok := strings.CutPrefix(strings.TrimSpace(h), "bytes=")
	if !ok {
		return 0
	}
	a, _, _ := strings.Cut(v, "-")
	n, err := strconv.ParseInt(strings.TrimSpace(a), 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

type countingWriter struct {
	http.ResponseWriter
	n       int64
	onWrite func(total int64)
	last    int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.ResponseWriter.Write(p)
	c.n += int64(n)
	if c.onWrite != nil && c.n-c.last >= 256<<10 {
		c.last = c.n
		c.onWrite(c.n)
	}
	return n, err
}

// transcode streams ffmpeg's output (MPEG-TS, or MP3 for music). It seeks
// by time (TimeSeekRange.dlna.org, DLNA.ORG_OP=10); byte ranges are ignored
// and the whole stream is sent, which HTTP allows.
func (s *Server) transcode(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("fileId"))
	if err != nil || !s.signed(r, "t", id.String()) {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return
	}
	ctx := r.Context()
	_, v := s.Current(ctx)
	row, err := s.fileRow(ctx, id)
	if err != nil || v == nil {
		http.NotFound(w, r)
		return
	}
	q := r.URL.Query()
	prof := ProfileByName(q.Get("p"))
	audio, err := strconv.Atoi(q.Get("a"))
	if err != nil || audio >= len(row.Audio) || audio < -1 {
		audio = library.PickAudio(row.Audio, v.Prefs.AudioLanguage)
	}
	if len(row.Audio) == 0 {
		audio = -1
	}
	startMS, _ := strconv.ParseInt(q.Get("s"), 10, 64)
	startMS = max(startMS, 0)
	var dur int64
	if row.DurationMS != nil {
		dur = *row.DurationMS
	}
	seekHdr := r.Header.Get(timeSeekHeader)
	var seek TimeSeek
	if seekHdr != "" {
		ts, ok := ParseTimeSeek(seekHdr)
		if !ok || (dur > 0 && startMS+ts.Start.Milliseconds() >= dur) {
			http.Error(w, "invalid time seek range", http.StatusRequestedRangeNotSatisfiable)
			return
		}
		seek = ts
	}
	plan := PlanFor(row.MediaFile, audio, prof, max(startMS, 1))
	if !allowed(v, plan) {
		http.Error(w, "this file needs transcoding, which the DLNA user may not use", http.StatusForbidden)
		return
	}
	at := startMS + seek.Start.Milliseconds()
	audioOnly := row.VideoCodec == ""
	mime, pn, format := "video/mpeg", "", "mpegts"
	if audioOnly {
		mime, pn, format = "audio/mpeg", "MP3", "mp3"
	}
	s.mediaHeaders(w, r, mime, Features(pn, false, true), row, startMS == 0)
	setExact(w.Header(), realTimeHeader, "DLNA.ORG_TLAG=*")
	if seekHdr != "" {
		// Positions are relative to the resource, which may itself start later (resume).
		setExact(w.Header(), timeSeekHeader, timeSeekReply(seek.Start.Milliseconds(), dur-startMS))
	}
	w.Header().Set("Accept-Ranges", "none")
	if r.Method == http.MethodHead {
		w.WriteHeader(http.StatusOK)
		return
	}
	enc := stream.DefaultEncoding()
	if s.Encoding != nil {
		enc = s.Encoding(ctx).Normalize()
	}
	// Every ffmpeg counts against the server-wide cap, remuxes included.
	if int(s.running.Load())+s.Stream.HLS.Active() >= enc.SessionLimit() {
		http.Error(w, "too many transcodes at once", http.StatusServiceUnavailable)
		return
	}
	path, err := s.Stream.FilePath(ctx, id) // re-validated with library.ResolveSafePath
	if err != nil {
		http.NotFound(w, r)
		return
	}
	args := stream.ProgressiveArgs(stream.Job{Input: path, StartMS: at, AudioIndex: audio, Plan: plan}, enc, format)
	if seek.End > 0 {
		args = append(args[:len(args)-1], "-t", npt(seek.End.Milliseconds()-seek.Start.Milliseconds()), args[len(args)-1])
	}
	cmd := exec.CommandContext(ctx, s.FFmpeg, args...) // #nosec G204 -- fixed binary, validated path
	var stderr bytes.Buffer
	cmd.Stderr = &limited{w: &stderr, n: 4096}
	out, err := cmd.StdoutPipe()
	if err != nil {
		http.Error(w, "transcoder failed", http.StatusInternalServerError)
		return
	}
	if err := cmd.Start(); err != nil {
		s.Log.Warn("dlna: ffmpeg did not start", "err", err)
		http.Error(w, "transcoder failed", http.StatusBadGateway)
		return
	}
	s.running.Add(1)
	defer s.running.Add(-1)
	play := s.tracker.start(s, r, row.ItemID, dur, prof.Label, plan.Method)
	began := time.Now()
	w.WriteHeader(http.StatusOK)
	buf := make([]byte, 256<<10)
	rc := http.NewResponseController(w)
	for {
		n, rerr := out.Read(buf)
		if n > 0 {
			// A renderer that stops reading (paused, switched off without closing the
			// connection) would otherwise hold ffmpeg and a transcode slot for good.
			_ = rc.SetWriteDeadline(time.Now().Add(progressiveWriteTimeout))
			if _, werr := w.Write(buf[:n]); werr != nil {
				break
			}
			_ = rc.Flush()
			play.at(min(dur, at+time.Since(began).Milliseconds()))
		}
		if rerr != nil {
			break
		}
	}
	if cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	if err := cmd.Wait(); err != nil && ctx.Err() == nil && stderr.Len() > 0 {
		s.Log.Debug("dlna: ffmpeg ended", "err", err, "stderr", stderr.String())
	}
}

type limited struct {
	w *bytes.Buffer
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if room := l.n - l.w.Len(); room > 0 {
		l.w.Write(p[:min(len(p), room)])
	}
	return len(p), nil
}

// subtitle converts a track to SRT (the format TVs accept as a sidecar).
func (s *Server) subtitle(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("fileId"))
	if err != nil || !s.signed(r, "s", id.String()) {
		http.Error(w, "invalid or expired link", http.StatusForbidden)
		return
	}
	idx, err := strconv.Atoi(strings.TrimSuffix(r.PathValue("name"), ".srt"))
	if err != nil || idx < 0 {
		http.NotFound(w, r)
		return
	}
	setExact(w.Header(), transferHeader, transferImage)
	s.Stream.ServeSubtitle(w, r, id, idx, "srt") // validates paths with ResolveSafePath
}

// art serves a poster: "tn.jpg" scaled to fit 160×160 (DLNA JPEG_TN), anything else as stored.
func (s *Server) art(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("itemId"))
	if err != nil || !s.signed(r, "a", id.String()) || s.Library.ImageDir == "" {
		http.NotFound(w, r)
		return
	}
	// Built from a UUID and a fixed word: no client text in the path.
	path := metadata.ImagePath(s.Library.ImageDir, id, "poster")
	raw, err := os.ReadFile(path) // #nosec G304 -- see above
	if err != nil {
		http.NotFound(w, r)
		return
	}
	pn := "JPEG_MED"
	if r.PathValue("name") == "tn.jpg" {
		raw, pn = s.thumbnail(id, raw), thumbnailProfile
	}
	w.Header().Set("Content-Type", "image/jpeg")
	setExact(w.Header(), transferHeader, transferImage)
	setExact(w.Header(), featuresHeader, imageFeatures(pn))
	w.Header().Set("Cache-Control", "private, max-age=86400")
	http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(raw))
}

// progressiveWriteTimeout ends a transcoded stream whose renderer has stopped
// reading for this long.
const progressiveWriteTimeout = 2 * time.Minute

var (
	thumbMu    sync.Mutex
	thumbCache = map[uuid.UUID][]byte{}
)

// thumbnail returns the poster scaled to fit 160×160 (cached in memory).
func (s *Server) thumbnail(id uuid.UUID, raw []byte) []byte {
	thumbMu.Lock()
	if b, ok := thumbCache[id]; ok {
		thumbMu.Unlock()
		return b
	}
	thumbMu.Unlock()
	img, _, err := image.Decode(bytes.NewReader(raw))
	if err != nil {
		return raw
	}
	var out bytes.Buffer
	if jpeg.Encode(&out, Fit(img, 160), &jpeg.Options{Quality: 85}) != nil {
		return raw
	}
	thumbMu.Lock()
	if len(thumbCache) > 2000 {
		thumbCache = map[uuid.UUID][]byte{}
	}
	thumbCache[id] = out.Bytes()
	thumbMu.Unlock()
	return out.Bytes()
}

// Fit scales img down (box filter) to fit a size×size square.
func Fit(img image.Image, size int) image.Image {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= size && h <= size || w == 0 || h == 0 {
		return img
	}
	nw, nh := size, h*size/w
	if h > w {
		nw, nh = w*size/h, size
	}
	nw, nh = max(nw, 1), max(nh, 1)
	dst := image.NewRGBA(image.Rect(0, 0, nw, nh))
	for y := 0; y < nh; y++ {
		y0, y1 := b.Min.Y+y*h/nh, b.Min.Y+max((y+1)*h/nh, y*h/nh+1)
		for x := 0; x < nw; x++ {
			x0, x1 := b.Min.X+x*w/nw, b.Min.X+max((x+1)*w/nw, x*w/nw+1)
			var r, g, bl, a, n uint64
			for yy := y0; yy < y1; yy++ {
				for xx := x0; xx < x1; xx++ {
					cr, cg, cb, ca := img.At(xx, yy).RGBA()
					r, g, bl, a, n = r+uint64(cr), g+uint64(cg), bl+uint64(cb), a+uint64(ca), n+1
				}
			}
			i := dst.PixOffset(x, y)
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2], dst.Pix[i+3] = uint8(r/n>>8), uint8(g/n>>8), uint8(bl/n>>8), uint8(a/n>>8) // #nosec G115 -- 16-bit channel >> 8
		}
	}
	return dst
}
