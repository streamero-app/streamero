package stream

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job describes one HLS stream to produce.
type Job struct {
	Input      string
	StartMS    int64
	AudioIndex int // -1 = no audio
	Plan       Plan
}

type session struct {
	dir     string
	cmd     *exec.Cmd
	cancel  context.CancelFunc
	lastHit time.Time
	done    chan struct{}
}

// Manager runs ffmpeg HLS sessions under Dir (a tmpfs in production).
type Manager struct {
	Dir    string
	FFmpeg string
	Log    *slog.Logger
	Idle   time.Duration // stop a session after this long without requests (default 90s)
	// Encoding returns the administrator's transcoding settings (defaults when nil).
	Encoding Loader
	mu       sync.Mutex
	streams  map[string]*session
}

// ErrNoSession is returned for unknown or finished sessions.
var ErrNoSession = errors.New("stream session not found")

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Args builds the ffmpeg command line for a job (exported for tests).
func Args(j Job, dir string, enc Encoding) []string {
	enc = enc.Normalize()
	a := codecArgs(j, enc)
	return append(a, "-f", "hls", "-hls_time", strconv.Itoa(enc.SegmentSeconds), "-hls_playlist_type", "event", "-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4", "-hls_segment_filename", filepath.Join(dir, "seg_%05d.m4s"),
		"-start_number", "0", filepath.Join(dir, "index.m3u8"))
}

// ProgressiveArgs builds an ffmpeg command line that writes one continuous
// stream to stdout, for players that want a plain HTTP body instead of HLS
// (DLNA renderers). Video goes out as MPEG-TS; audio-only plans as format
// ("mp3" or "adts").
func ProgressiveArgs(j Job, enc Encoding, format string) []string {
	enc = enc.Normalize()
	if j.Plan.AudioOnly {
		a := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
		if j.StartMS > 0 {
			a = append(a, "-ss", strconv.FormatFloat(float64(j.StartMS)/1000, 'f', 3, 64))
		}
		a = append(a, "-i", j.Input, "-vn", "-map", fmt.Sprintf("0:a:%d?", max(j.AudioIndex, 0)))
		// Audio is always re-encoded: the source codec need not fit the output format.
		switch format {
		case "mp3":
			a = append(a, "-c:a", "libmp3lame", "-b:a", "320k")
		default:
			format = "adts"
			a = append(a, "-c:a", "aac", "-b:a", fmt.Sprintf("%dk", enc.AudioKbps))
		}
		return append(a, "-f", format, "pipe:1")
	}
	return append(codecArgs(j, enc), "-f", "mpegts", "-muxdelay", "0", "pipe:1")
}

// codecArgs is everything before the output format: input, seeking, stream
// selection and encoders.
func codecArgs(j Job, enc Encoding) []string {
	a := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
	var encode []string
	videoMap := "0:v:0"
	var graph string // -filter_complex when a subtitle is burned in
	burn := j.Plan.Burn
	if !j.Plan.TranscodeVideo {
		burn = nil
	}
	if j.Plan.TranscodeVideo {
		var pre, post []string
		if j.Plan.ToneMap && enc.ToneMaps() {
			// HDR (PQ/HLG) to SDR BT.709 with the Hable curve.
			pre = append(pre, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
				"tonemap=tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv")
		}
		if j.Plan.Height > 0 {
			// Scaled after a burned-in subtitle, so the subtitle keeps its proportions.
			post = append(post, fmt.Sprintf("scale=-2:%d", j.Plan.Height))
		}
		if burn != nil && !burn.Bitmap {
			// libass renders text tracks (ASS/SSA styling and attached fonts included).
			// It reads the file itself in original time, so a seeked stream is shifted
			// back to the file's clock around it.
			pre = append(pre, subtitlesFilter(burnSource(j, dir), burn, j.StartMS)...)
			burn = nil
		}
		in, chain, codec := enc.videoArgs(append(pre, post...))
		a = append(a, in...)
		if burn != nil {
			// Bitmap track over the picture, centred and bottom-aligned: PGS/VobSub canvases
			// are the video size or wider (e.g. 1080p subtitles on a 2.40:1 crop).
			_, chain, _ = enc.videoArgs(post)
			sub := fmt.Sprintf("[0:s:%d]", burn.Stream)
			if burn.External {
				sub = "[1:s:0]"
			}
			base := "[0:v:0]"
			if len(pre) > 0 {
				graph += "[0:v:0]" + join(pre) + "[base];"
				base = "[base]"
			}
			graph += base + sub + "overlay=x=(W-w)/2:y=H-h:eof_action=pass:format=auto," + join(chain) + "[vout]"
			videoMap = "[vout]"
			encode = codec
		} else {
			encode = append([]string{"-vf", join(chain)}, codec...)
		}
		encode = append(encode, "-g", enc.gop(), "-keyint_min", enc.gop(), "-sc_threshold", "0")
		if j.Plan.MaxBitrate > 0 {
			encode = append(encode, "-maxrate", strconv.Itoa(j.Plan.MaxBitrate), "-bufsize", strconv.Itoa(j.Plan.MaxBitrate*2))
		}
	} else {
		encode = []string{"-c:v", "copy"}
	}
	if j.StartMS > 0 {
		a = append(a, "-ss", strconv.FormatFloat(float64(j.StartMS)/1000, 'f', 3, 64))
	}
	a = append(a, "-i", j.Input)
	if burn != nil && burn.External {
		// A sidecar is read from its start, moved to the stream's clock: seeking a
		// .sup would skip the subtitle already on screen at the start position.
		if j.StartMS > 0 {
			a = append(a, "-itsoffset", "-"+strconv.FormatFloat(float64(j.StartMS)/1000, 'f', 3, 64))
		}
		a = append(a, "-i", burn.Path)
	}
	if graph != "" {
		a = append(a, "-filter_complex", graph)
	}
	if j.Plan.AudioOnly {
		encode = []string{"-vn"}
	} else {
		a = append(a, "-map", videoMap)
	}
	if j.AudioIndex >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:a:%d?", j.AudioIndex))
	}
	a = append(a, encode...)
	if j.AudioIndex >= 0 {
		if j.Plan.TranscodeAudio {
			a = append(a, "-c:a", "aac", "-b:a", fmt.Sprintf("%dk", enc.AudioKbps), "-ac", "2")
		} else {
			a = append(a, "-c:a", "copy")
		}
	}
	return a
}

// Start launches ffmpeg and waits until the first segment is listed.
func (m *Manager) Start(ctx context.Context, j Job) (string, error) {
	enc := DefaultEncoding()
	if m.Encoding != nil {
		enc = m.Encoding(ctx).Normalize()
	}
	if j.Plan.TranscodeVideo && enc.MaxSessions > 0 && m.Active() >= enc.MaxSessions {
		return "", ErrBusy
	}
	id := newID()
	dir := filepath.Join(m.Dir, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	if b := j.Plan.Burn; b != nil && j.Plan.TranscodeVideo && !b.Bitmap {
		target := j.Input
		if b.External {
			target = b.Path
		}
		if err := os.Symlink(target, burnSource(j, dir)); err != nil {
			_ = os.RemoveAll(dir)
			return "", fmt.Errorf("link subtitle source: %w", err)
		}
	}
	runCtx, cancel := context.WithCancel(context.Background())
	// #nosec G204 -- fixed binary; the input path was validated with ResolveSafePath.
	cmd := exec.CommandContext(runCtx, m.FFmpeg, Args(j, dir, enc)...)
	var stderr strings.Builder
	cmd.Stderr = &limitedWriter{w: &stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		cancel()
		_ = os.RemoveAll(dir)
		return "", fmt.Errorf("start ffmpeg: %w", err)
	}
	s := &session{dir: dir, cmd: cmd, cancel: cancel, lastHit: time.Now(), done: make(chan struct{})}
	go func() {
		err := cmd.Wait()
		if err != nil && runCtx.Err() == nil {
			m.Log.Warn("ffmpeg failed", "session", id, "err", err, "stderr", stderr.String())
		}
		close(s.done)
	}()
	m.mu.Lock()
	if m.streams == nil {
		m.streams = map[string]*session{}
	}
	m.streams[id] = s
	m.mu.Unlock()

	deadline := time.After(30 * time.Second)
	for {
		if playlistReady(filepath.Join(dir, "index.m3u8")) {
			return id, nil
		}
		select {
		case <-ctx.Done():
			m.stop(id)
			return "", ctx.Err()
		case <-s.done:
			if playlistReady(filepath.Join(dir, "index.m3u8")) {
				return id, nil // very short file: already finished
			}
			m.stop(id)
			return "", fmt.Errorf("ffmpeg exited: %s", strings.TrimSpace(stderr.String()))
		case <-deadline:
			m.stop(id)
			return "", errors.New("transcoder did not produce a segment in time")
		case <-time.After(150 * time.Millisecond):
		}
	}
}

// burnSource is the path the subtitles filter reads: a link inside the session
// directory (created by Start) to the sidecar file or to the video itself, so the
// filter argument never contains the media path's characters.
func burnSource(j Job, dir string) string {
	target := j.Input
	if j.Plan.Burn != nil && j.Plan.Burn.External {
		target = j.Plan.Burn.Path
	}
	return filepath.Join(dir, "burn"+strings.ToLower(filepath.Ext(target)))
}

// subtitlesFilter draws a text subtitle track with libass. startMS is where the
// stream starts in the file: the filter matches subtitle times against frame
// times, so frames are moved to the file's clock and back.
func subtitlesFilter(path string, b *BurnIn, startMS int64) []string {
	f := "subtitles=filename=" + filterValue(path)
	if !b.External {
		f += ":si=" + strconv.Itoa(b.Stream)
	}
	if startMS <= 0 {
		return []string{f}
	}
	off := strconv.FormatFloat(float64(startMS)/1000, 'f', 3, 64)
	return []string{"setpts=PTS+" + off + "/TB", f, "setpts=PTS-" + off + "/TB"}
}

// filterValue escapes a filter option value for a filtergraph: first for the
// option parser (\ ' :), then for the graph parser (\ ' [ ] , ;).
func filterValue(v string) string {
	v = strings.NewReplacer(`\`, `\\`, `'`, `\'`, `:`, `\:`).Replace(v)
	return strings.NewReplacer(`\`, `\\`, `'`, `\'`, `[`, `\[`, `]`, `\]`, `,`, `\,`, `;`, `\;`).Replace(v)
}

func playlistReady(path string) bool {
	b, err := os.ReadFile(path) // #nosec G304 -- inside the manager's own directory
	return err == nil && strings.Contains(string(b), "#EXTINF")
}

// File returns the path of a session file and marks the session active.
// name must be a plain file name produced by ffmpeg.
func (m *Manager) File(id, name string) (string, error) {
	if name == "" || strings.ContainsAny(name, `/\`) || strings.HasPrefix(name, ".") {
		return "", ErrNoSession
	}
	m.mu.Lock()
	s, ok := m.streams[id]
	if ok {
		s.lastHit = time.Now()
	}
	m.mu.Unlock()
	if !ok {
		return "", ErrNoSession
	}
	// name has no separators or leading dot (checked above) and s.dir is our own session directory.
	p := filepath.Join(s.dir, name)
	// Segments may still be in progress: wait briefly for the next one.
	for i := 0; i < 100; i++ {
		if _, err := os.Stat(p); err == nil { // #nosec G703 -- see above
			return p, nil
		}
		select {
		case <-s.done:
			if _, err := os.Stat(p); err == nil { // #nosec G703 -- see above
				return p, nil
			}
			return "", ErrNoSession
		case <-time.After(100 * time.Millisecond):
		}
	}
	return "", ErrNoSession
}

// Playlist returns the playlist with every URI signed (query appended).
func (m *Manager) Playlist(id, query string) ([]byte, error) {
	p, err := m.File(id, "index.m3u8")
	if err != nil {
		return nil, err
	}
	f, err := os.Open(p) // #nosec G304 -- session directory
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	var out strings.Builder
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.HasPrefix(line, "#EXT-X-MAP:URI=\""):
			name := strings.TrimSuffix(strings.TrimPrefix(line, "#EXT-X-MAP:URI=\""), "\"")
			line = "#EXT-X-MAP:URI=\"" + name + "?" + query + "\""
		case line != "" && !strings.HasPrefix(line, "#"):
			line = line + "?" + query
		}
		out.WriteString(line + "\n")
	}
	return []byte(out.String()), sc.Err()
}

func (m *Manager) stop(id string) {
	m.mu.Lock()
	s, ok := m.streams[id]
	delete(m.streams, id)
	m.mu.Unlock()
	if !ok {
		return
	}
	s.cancel()
	<-s.done
	_ = os.RemoveAll(s.dir)
}

// Reap stops idle sessions until ctx ends; call once in a goroutine.
func (m *Manager) Reap(ctx context.Context) {
	idle := m.Idle
	if idle == 0 {
		idle = 90 * time.Second
	}
	t := time.NewTicker(10 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			m.mu.Lock()
			ids := make([]string, 0, len(m.streams))
			for id := range m.streams {
				ids = append(ids, id)
			}
			m.mu.Unlock()
			for _, id := range ids {
				m.stop(id)
			}
			return
		case now := <-t.C:
			m.mu.Lock()
			var stale []string
			for id, s := range m.streams {
				if now.Sub(s.lastHit) > idle {
					stale = append(stale, id)
				}
			}
			m.mu.Unlock()
			for _, id := range stale {
				m.Log.Debug("stopping idle stream", "session", id)
				m.stop(id)
			}
		}
	}
}

// Active returns the number of running sessions.
func (m *Manager) Active() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.streams)
}

type limitedWriter struct {
	w interface{ WriteString(string) (int, error) }
	n int
}

func (l *limitedWriter) Write(p []byte) (int, error) {
	if l.n > 0 {
		chunk := p
		if len(chunk) > l.n {
			chunk = chunk[:l.n]
		}
		_, _ = l.w.WriteString(string(chunk))
		l.n -= len(chunk)
	}
	return len(p), nil
}
