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
	Dir     string
	FFmpeg  string
	Log     *slog.Logger
	Idle    time.Duration // stop a session after this long without requests (default 90s)
	mu      sync.Mutex
	streams map[string]*session
}

// ErrNoSession is returned for unknown or finished sessions.
var ErrNoSession = errors.New("stream session not found")

func newID() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Args builds the ffmpeg command line for a job (exported for tests).
func Args(j Job, dir string) []string {
	a := []string{"-nostdin", "-hide_banner", "-loglevel", "error"}
	if j.StartMS > 0 {
		a = append(a, "-ss", strconv.FormatFloat(float64(j.StartMS)/1000, 'f', 3, 64))
	}
	a = append(a, "-i", j.Input, "-map", "0:v:0")
	if j.AudioIndex >= 0 {
		a = append(a, "-map", fmt.Sprintf("0:a:%d?", j.AudioIndex))
	}
	if j.Plan.TranscodeVideo {
		var filters []string
		if j.Plan.ToneMap {
			// HDR (PQ/HLG) to SDR BT.709 with the Hable curve.
			filters = append(filters, "zscale=t=linear:npl=100", "format=gbrpf32le", "zscale=p=bt709",
				"tonemap=tonemap=hable:desat=0", "zscale=t=bt709:m=bt709:r=tv")
		}
		if j.Plan.Height > 0 {
			filters = append(filters, fmt.Sprintf("scale=-2:%d", j.Plan.Height))
		}
		filters = append(filters, "format=yuv420p")
		a = append(a, "-vf", strings.Join(filters, ","), "-c:v", "libx264", "-preset", "veryfast", "-crf", "22",
			"-profile:v", "high", "-g", "48", "-keyint_min", "48", "-sc_threshold", "0")
	} else {
		a = append(a, "-c:v", "copy")
	}
	if j.AudioIndex >= 0 {
		if j.Plan.TranscodeAudio {
			a = append(a, "-c:a", "aac", "-b:a", "192k", "-ac", "2")
		} else {
			a = append(a, "-c:a", "copy")
		}
	}
	return append(a, "-f", "hls", "-hls_time", "6", "-hls_playlist_type", "event", "-hls_segment_type", "fmp4",
		"-hls_fmp4_init_filename", "init.mp4", "-hls_segment_filename", filepath.Join(dir, "seg_%05d.m4s"),
		"-start_number", "0", filepath.Join(dir, "index.m3u8"))
}

// Start launches ffmpeg and waits until the first segment is listed.
func (m *Manager) Start(ctx context.Context, j Job) (string, error) {
	id := newID()
	dir := filepath.Join(m.Dir, id)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	runCtx, cancel := context.WithCancel(context.Background())
	// #nosec G204 -- fixed binary; the input path was validated with ResolveSafePath.
	cmd := exec.CommandContext(runCtx, m.FFmpeg, Args(j, dir)...)
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
