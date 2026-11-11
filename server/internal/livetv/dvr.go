package livetv

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/streamero-app/streamero/server/internal/library"
)

// Run is the DVR loop: it expands series rules, resolves tuner conflicts,
// starts and ends recordings and applies retention. Call once (serve process).
func (s *Service) Run(ctx context.Context) {
	s.mu.Lock()
	s.kick = make(chan struct{}, 1)
	s.mu.Unlock()
	// Recordings that were running when the server stopped continue (appending to their file).
	_, _ = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'scheduled' WHERE status = 'recording'`)
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	var lastRetention time.Time
	for {
		if err := s.Schedule(ctx); err != nil && ctx.Err() == nil {
			s.log().Warn("dvr schedule failed", "err", err)
		}
		s.startDue(ctx)
		if s.now().Sub(lastRetention) > time.Hour {
			lastRetention = s.now()
			if err := s.ApplyRetention(ctx); err != nil && ctx.Err() == nil {
				s.log().Warn("dvr retention failed", "err", err)
			}
		}
		select {
		case <-ctx.Done():
			s.stopAllRecorders()
			return
		case <-tick.C:
		case <-s.kick:
		}
	}
}

var scheduleMu sync.Mutex

// Schedule brings the schedule up to date: programme times follow guide
// changes, series rules get their new airings, missed recordings fail and
// tuner conflicts are resolved.
func (s *Service) Schedule(ctx context.Context) error {
	scheduleMu.Lock()
	defer scheduleMu.Unlock()
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_recordings r SET start_at = p.start_at, end_at = p.end_at
		FROM livetv_programs p WHERE r.program_id = p.id AND r.status IN ('scheduled', 'conflict')
			AND (r.start_at <> p.start_at OR r.end_at <> p.end_at)`); err != nil {
		return err
	}
	if err := s.expandRules(ctx); err != nil {
		return err
	}
	now := s.now()
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'failed', finished_at = now(),
			error = CASE WHEN status = 'conflict' THEN 'no free tuner: ' || error ELSE 'missed: the server was not running' END
		WHERE status IN ('scheduled', 'conflict') AND end_at + make_interval(secs => post_padding) <= $1`, now); err != nil {
		return err
	}
	return s.resolve(ctx)
}

func (s *Service) expandRules(ctx context.Context) error {
	rules, err := s.SeriesRules(ctx)
	if err != nil {
		return err
	}
	st := s.LoadSettings(ctx)
	now := s.now()
	for _, rule := range rules {
		rows, err := s.Pool.Query(ctx, `SELECT c.id, `+programCols+` FROM livetv_programs p
			JOIN livetv_channels c ON c.guide_id = p.guide_id AND c.guide_channel = p.xmltv_channel
			JOIN livetv_tuners t ON t.id = c.tuner_id AND t.enabled
			WHERE lower(p.title) = lower($1) AND p.end_at > $2 ORDER BY p.start_at, c.sort_number`, rule.Title, now)
		if err != nil {
			return err
		}
		var airings []Airing
		progs := map[uuid.UUID]Program{}
		chanOf := map[uuid.UUID]uuid.UUID{}
		for rows.Next() {
			var cid uuid.UUID
			p, err := s.scanProgram(rows, &cid)
			if err != nil {
				rows.Close()
				return err
			}
			key := p.ID
			if _, ok := progs[key]; !ok || (rule.ChannelID != nil && cid == *rule.ChannelID) {
				progs[key] = p
				chanOf[key] = cid
			}
			airings = append(airings, Airing{ProgramID: p.ID, ChannelID: cid, Title: p.Title, Start: p.Start, IsNew: p.IsNew,
				IsRepeat: p.IsRepeat, EpisodeKey: p.EpisodeKey})
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return err
		}
		if len(airings) == 0 {
			continue
		}
		have := map[string]bool{}
		krows, err := s.Pool.Query(ctx, `SELECT DISTINCT episode_key FROM livetv_recordings WHERE episode_key <> ''
			AND (series_rule_id = $1 OR lower(title) = lower($2)) AND status IN ('scheduled', 'conflict', 'recording', 'completed', 'deleted')`,
			rule.ID, rule.Title)
		if err != nil {
			return err
		}
		keys, err := pgx.CollectRows(krows, pgx.RowTo[string])
		if err != nil {
			return err
		}
		for _, k := range keys {
			have[k] = true
		}
		// Airings already held by this rule do not count as "have".
		hrows, err := s.Pool.Query(ctx, `SELECT program_id FROM livetv_recordings WHERE series_rule_id = $1 AND program_id IS NOT NULL`, rule.ID)
		if err != nil {
			return err
		}
		held, err := pgx.CollectRows(hrows, pgx.RowTo[uuid.UUID])
		if err != nil {
			return err
		}
		heldSet := map[uuid.UUID]bool{}
		for _, id := range held {
			heldSet[id] = true
		}
		spec := RuleSpec{Title: rule.Title, ChannelID: rule.ChannelID, NewOnly: rule.NewOnly}
		// Prefer the rule's channel for every programme.
		if rule.ChannelID == nil {
			for i := range airings {
				airings[i].ChannelID = chanOf[airings[i].ProgramID]
			}
		}
		for _, a := range SelectAirings(spec, airings, have) {
			if heldSet[a.ProgramID] {
				continue
			}
			pre, post := st.PrePadding, st.PostPadding
			if rule.PrePadding != nil {
				pre = *rule.PrePadding
			}
			if rule.PostPadding != nil {
				post = *rule.PostPadding
			}
			ruleID := rule.ID
			ch := Channel{ID: a.ChannelID}
			_ = s.Pool.QueryRow(ctx, `SELECT name, number FROM livetv_channels WHERE id = $1`, a.ChannelID).Scan(&ch.Name, &ch.Number)
			if _, err := s.insertRecording(ctx, s.Pool, progs[a.ProgramID], ch, &ruleID, pre, post, rule.Priority, uuid.Nil); err != nil {
				return err
			}
		}
	}
	return nil
}

// resolve runs ResolveConflicts over every upcoming and running recording.
func (s *Service) resolve(ctx context.Context) error {
	rows, err := s.Pool.Query(ctx, `SELECT r.id, c.tuner_id, t.tuner_count, r.start_at, r.end_at, r.pre_padding, r.post_padding,
			r.priority, r.created_at, r.status, r.title
		FROM livetv_recordings r JOIN livetv_channels c ON c.id = r.channel_id JOIN livetv_tuners t ON t.id = c.tuner_id
		WHERE r.status IN ('scheduled', 'conflict', 'recording')`)
	if err != nil {
		return err
	}
	var slots []Slot
	caps := map[uuid.UUID]int{}
	status := map[uuid.UUID]string{}
	titles := map[uuid.UUID]string{}
	for rows.Next() {
		var sl Slot
		var capacity, pre, post int
		var start, end time.Time
		var st, title string
		if err := rows.Scan(&sl.ID, &sl.Tuner, &capacity, &start, &end, &pre, &post, &sl.Priority, &sl.Created, &st, &title); err != nil {
			rows.Close()
			return err
		}
		sl.Start, sl.End = Window(start, end, pre, post)
		sl.Running = st == "recording"
		caps[sl.Tuner] = capacity
		status[sl.ID] = st
		titles[sl.ID] = title
		slots = append(slots, sl)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	res := ResolveConflicts(slots, func(t uuid.UUID) int { return caps[t] })
	batch := &pgx.Batch{}
	for _, sl := range slots {
		if sl.Running {
			continue
		}
		if res.Accepted[sl.ID] {
			if status[sl.ID] != "scheduled" {
				batch.Queue(`UPDATE livetv_recordings SET status = 'scheduled', error = '' WHERE id = $1 AND status = 'conflict'`, sl.ID)
			}
			continue
		}
		var names []string
		for _, o := range res.ConflictsWith[sl.ID] {
			names = append(names, titles[o])
		}
		msg := "conflicts with " + strings.Join(names, ", ")
		batch.Queue(`UPDATE livetv_recordings SET status = 'conflict', error = $2 WHERE id = $1 AND status IN ('scheduled', 'conflict')`, sl.ID, msg)
	}
	// Recordings whose channel disappeared cannot happen.
	batch.Queue(`UPDATE livetv_recordings SET status = 'failed', error = 'the channel no longer exists', finished_at = now()
		WHERE status IN ('scheduled', 'conflict') AND channel_id IS NULL`)
	return s.Pool.SendBatch(ctx, batch).Close()
}

// recorder is a running recording.
type recorder struct {
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	end    time.Time
	reason string
}

func (r *recorder) deadline() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.end
}

func (s *Service) startDue(ctx context.Context) {
	rows, err := s.Pool.Query(ctx, `SELECT `+recordingCols+` FROM livetv_recordings r WHERE r.status = 'scheduled'
		AND r.start_at - make_interval(secs => r.pre_padding) <= $1 AND r.end_at + make_interval(secs => r.post_padding) > $1`, s.now())
	if err != nil {
		return
	}
	var due []Recording
	for rows.Next() {
		r, err := s.scanRecording(rows)
		if err == nil {
			due = append(due, r)
		}
	}
	rows.Close()
	for _, r := range due {
		s.mu.Lock()
		_, running := s.recorders[r.ID]
		s.mu.Unlock()
		if !running {
			if err := s.startRecorder(ctx, r); err != nil {
				s.log().Warn("recording could not start", "title", r.Title, "err", err)
			}
		}
	}
}

type recChannel struct {
	tuner     uuid.UUID
	capacity  int
	url       string
	userAgent string
}

func (s *Service) startRecorder(ctx context.Context, r Recording) error {
	if r.ChannelID == nil {
		return errors.New("no channel")
	}
	var ch recChannel
	if err := s.Pool.QueryRow(ctx, `SELECT c.tuner_id, t.tuner_count, c.stream_url, t.user_agent FROM livetv_channels c
		JOIN livetv_tuners t ON t.id = c.tuner_id WHERE c.id = $1`, *r.ChannelID).Scan(&ch.tuner, &ch.capacity, &ch.url, &ch.userAgent); err != nil {
		return err
	}
	l, err := s.acquire(ch.tuner, ch.capacity, true, nil)
	if err != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'conflict', error = 'no free tuner' WHERE id = $1`, r.ID)
		return err
	}
	if r.Path == "" {
		r.Path = s.recordingPath(r)
		if _, err := s.Pool.Exec(ctx, `UPDATE livetv_recordings SET path = $2 WHERE id = $1`, r.ID, r.Path); err != nil {
			s.release(l)
			return err
		}
	}
	if err := os.MkdirAll(filepath.Dir(r.Path), 0o750); err != nil {
		s.release(l)
		_, _ = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'failed', error = $2, finished_at = now() WHERE id = $1`, r.ID, err.Error())
		return err
	}
	if _, err := s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'recording', error = '' WHERE id = $1`, r.ID); err != nil {
		s.release(l)
		return err
	}
	_, end := Window(r.Start, r.End, r.PrePadding, r.PostPadding)
	rctx, cancel := context.WithCancel(context.Background())
	rec := &recorder{cancel: cancel, done: make(chan struct{}), end: end}
	s.mu.Lock()
	if s.recorders == nil {
		s.recorders = map[uuid.UUID]*recorder{}
	}
	s.recorders[r.ID] = rec
	s.mu.Unlock()
	s.log().Info("recording started", "title", r.Title, "channel", r.ChannelName, "path", r.Path, "until", end)
	go func() { //nolint:gosec // G118: a recording outlives the request or tick that started it
		defer close(rec.done)
		err := s.capture(rctx, rec, r.Path, ch)
		s.release(l)
		s.mu.Lock()
		delete(s.recorders, r.ID)
		s.mu.Unlock()
		s.finish(context.Background(), r.ID, rec, err)
	}()
	return nil
}

// capture writes the channel to path (appending) until the deadline. When the
// source drops, it reconnects while time is left.
func (s *Service) capture(ctx context.Context, rec *recorder, path string, ch recChannel) error {
	//nolint:gosec // G302/G304: path built by recordingPath under the recordings folder; group-readable for the library and post-processing
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o640)
	if err != nil {
		return err
	}
	defer func() { _ = f.Close() }()
	var lastErr error
	for {
		left := time.Until(rec.deadline())
		if left <= 0 || ctx.Err() != nil {
			return nil
		}
		runCtx, cancel := context.WithCancel(ctx)
		args := append([]string{"-nostdin", "-hide_banner", "-loglevel", "error"}, inputArgs(ch.url, ch.userAgent)...)
		args = append(args, "-i", ch.url, "-map", "0:v?", "-map", "0:a?", "-map", "0:s?", "-c", "copy", "-f", "mpegts", "pipe:1")
		cmd := exec.CommandContext(runCtx, s.ffmpeg(), args...) // #nosec G204 -- fixed binary; URL scheme checked, protocols whitelisted
		cmd.Stdout = f
		var stderr strings.Builder
		cmd.Stderr = &limited{b: &stderr, n: 2048}
		cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
		cmd.WaitDelay = 5 * time.Second
		if err := cmd.Start(); err != nil {
			cancel()
			return err
		}
		exited := make(chan error, 1)
		go func() { exited <- cmd.Wait() }()
		stop := false
		for !stop {
			select {
			case err := <-exited:
				if err != nil {
					lastErr = fmt.Errorf("%w: %s", err, strings.TrimSpace(stderr.String()))
				}
				stop = true
			case <-time.After(500 * time.Millisecond):
				if !time.Now().Before(rec.deadline()) || ctx.Err() != nil {
					cancel()
					<-exited //nolint:errcheck // stopped on purpose; its exit status says nothing
					return nil
				}
			}
		}
		cancel()
		if !time.Now().Before(rec.deadline()) {
			return nil
		}
		s.log().Warn("recording source dropped; reconnecting", "path", path, "err", lastErr)
		select {
		case <-ctx.Done():
			return nil
		case <-time.After(3 * time.Second):
		}
	}
}

func (s *Service) ffmpeg() string {
	if s.FFmpeg != "" {
		return s.FFmpeg
	}
	return "ffmpeg"
}

type limited struct {
	b *strings.Builder
	n int
}

func (l *limited) Write(p []byte) (int, error) {
	if l.n > 0 {
		c := p[:min(len(p), l.n)]
		l.b.Write(c)
		l.n -= len(c)
	}
	return len(p), nil
}

// finish records the outcome, writes the .nfo, runs post-processing and
// makes the recording appear in the recordings library.
func (s *Service) finish(ctx context.Context, id uuid.UUID, rec *recorder, captureErr error) {
	r, err := s.Recording(ctx, id)
	if err != nil {
		return
	}
	var size int64
	if st, err := os.Stat(r.Path); err == nil {
		size = st.Size()
	}
	rec.mu.Lock()
	reason := rec.reason
	rec.mu.Unlock()
	status, msg := "completed", ""
	switch {
	case size == 0:
		status = "failed"
		msg = "nothing was recorded"
		if captureErr != nil {
			msg += ": " + captureErr.Error()
		}
		_ = os.Remove(r.Path)
	case reason != "":
		msg = "stopped early"
	}
	_, _ = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = $2, error = $3, size_bytes = $4, finished_at = now() WHERE id = $1`,
		id, status, msg, size)
	s.log().Info("recording finished", "title", r.Title, "status", status, "bytes", size)
	if status != "completed" {
		s.Kick()
		return
	}
	if err := s.writeNFO(r); err != nil {
		s.log().Warn("recording nfo not written", "path", r.Path, "err", err)
	}
	if perr := s.postProcess(ctx, r); perr != nil {
		_, _ = s.Pool.Exec(ctx, `UPDATE livetv_recordings SET post_process_error = $2 WHERE id = $1`, id, perr.Error())
	}
	s.scanLibrary(ctx)
	if err := s.ApplyRetention(ctx); err != nil {
		s.log().Warn("dvr retention failed", "err", err)
	}
	s.Kick()
}

// stopRecorder ends a running recording now (the file is kept).
func (s *Service) stopRecorder(id uuid.UUID, reason string) {
	s.mu.Lock()
	rec := s.recorders[id]
	s.mu.Unlock()
	if rec == nil {
		return
	}
	rec.mu.Lock()
	rec.reason = reason
	rec.end = time.Now()
	rec.mu.Unlock()
	<-rec.done
}

// updateRunningEnd applies a changed end padding to a running recording.
func (s *Service) updateRunningEnd(id uuid.UUID) {
	s.mu.Lock()
	rec := s.recorders[id]
	s.mu.Unlock()
	if rec == nil {
		return
	}
	var end time.Time
	var post int
	if err := s.Pool.QueryRow(context.Background(), `SELECT end_at, post_padding FROM livetv_recordings WHERE id = $1`, id).Scan(&end, &post); err != nil {
		return
	}
	_, e := Window(end, end, 0, post)
	rec.mu.Lock()
	rec.end = e
	rec.mu.Unlock()
}

func (s *Service) stopAllRecorders() {
	s.mu.Lock()
	recs := make([]*recorder, 0, len(s.recorders))
	for _, r := range s.recorders {
		recs = append(recs, r)
	}
	s.mu.Unlock()
	// Not marked as stopped: on the next start they continue (status goes back to scheduled).
	for _, r := range recs {
		r.cancel()
		<-r.done
	}
}

// WaitRecording blocks until a running recording has finished (tests).
func (s *Service) WaitRecording(ctx context.Context, id uuid.UUID) {
	s.mu.Lock()
	rec := s.recorders[id]
	s.mu.Unlock()
	if rec == nil {
		return
	}
	select {
	case <-rec.done:
	case <-ctx.Done():
	}
}

// --- files -------------------------------------------------------------------

// safeName makes a title usable as one path component.
func safeName(s string) string {
	s = strings.Map(func(r rune) rune {
		switch {
		case r < 32, r == 127:
			return -1
		case strings.ContainsRune(`/\:*?"<>|`, r):
			return ' '
		}
		return r
	}, s)
	s = strings.Join(strings.Fields(s), " ")
	s = strings.Trim(s, ". ")
	if len(s) > 120 {
		s = strings.TrimSpace(s[:120])
		for !utf8ValidEnd(s) {
			s = s[:len(s)-1]
		}
	}
	if s == "" || s == "." || s == ".." {
		return "Recording"
	}
	return s
}

func utf8ValidEnd(s string) bool {
	return s == "" || strings.ToValidUTF8(s, "") == s
}

// recordingPath lays recordings out the way the library scanner reads them:
//
//	<Title>/Season N/<Title> - SxxEyy - <Episode>.ts    (season and episode known)
//	Movies/<Title (Year)>/<Title (Year)>.ts             (films)
//	<Title>/<Title> - YYYY-MM-DD HH.MM - <Episode>.ts   (everything else: by air date)
func (s *Service) recordingPath(r Recording) string {
	title := safeName(r.Title)
	sub := ""
	if r.SubTitle != "" {
		sub = " - " + safeName(r.SubTitle)
	}
	var rel string
	switch {
	case r.Season != nil && r.Episode != nil:
		rel = filepath.Join(title, fmt.Sprintf("Season %d", *r.Season),
			fmt.Sprintf("%s - S%02dE%02d%s", title, *r.Season, *r.Episode, sub))
	case CategoryOf(r.Categories) == CatMovies && r.SeriesRuleID == nil:
		name := title
		if r.Year != nil {
			name += fmt.Sprintf(" (%d)", *r.Year)
		}
		rel = filepath.Join("Movies", name, name)
	default:
		local := r.Start.Local()
		rel = filepath.Join(title, fmt.Sprintf("%s - %s%s", title, local.Format("2006-01-02 15.04"), sub))
	}
	base := filepath.Join(s.RecordingsDir, rel)
	p := base + ".ts"
	for i := 2; ; i++ {
		if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
			return p
		}
		p = base + " (" + strconv.Itoa(i) + ").ts"
	}
}

// ensureLibrary returns the recordings library, creating it the first time.
func (s *Service) ensureLibrary(ctx context.Context) (uuid.UUID, bool) {
	if s.Library == nil || s.RecordingsDir == "" {
		return uuid.Nil, false
	}
	st := s.LoadSettings(ctx)
	if st.LibraryID != nil {
		if _, err := s.Library.Get(ctx, *st.LibraryID); err == nil {
			return *st.LibraryID, true
		}
	}
	lib, err := s.Library.Create(ctx, "Recordings", "mixed", []string{s.RecordingsDir})
	if err != nil {
		s.log().Warn("recordings library not created", "dir", s.RecordingsDir, "err", err)
		return uuid.Nil, false
	}
	id := lib.ID
	st.LibraryID = &id
	if err := s.Settings().Set(ctx, KeySettings, st); err != nil {
		return id, true
	}
	return id, true
}

func (s *Service) scanLibrary(ctx context.Context) {
	if id, ok := s.ensureLibrary(ctx); ok {
		if err := s.Library.Scan(ctx, id); err != nil {
			s.log().Warn("recordings library scan not queued", "err", err)
		}
	}
}

// RecordingsLibrary returns the id of the recordings library, if created.
func (s *Service) RecordingsLibrary(ctx context.Context) *uuid.UUID {
	return s.LoadSettings(ctx).LibraryID
}

// recordingFile checks that a stored recording path is a file inside the
// recordings folder; nothing else is ever deleted.
func (s *Service) recordingFile(p string) (string, error) {
	if s.RecordingsDir == "" || p == "" {
		return "", library.ErrOutsideRoots
	}
	resolved, err := library.ResolveSafePath(library.Roots{s.RecordingsDir}, p)
	if err != nil {
		return "", err
	}
	if filepath.Clean(resolved) == filepath.Clean(s.RecordingsDir) {
		return "", library.ErrOutsideRoots
	}
	if !strings.EqualFold(filepath.Ext(resolved), ".ts") {
		return "", library.ErrOutsideRoots
	}
	return resolved, nil
}

// ApplyRetention deletes finished recordings by the admin's opt-in rules:
// older than deleteAfterDays, or beyond the newest keepLast of a series rule.
// Only files the DVR recorded (paths it stored, inside the recordings folder)
// and the .nfo it wrote beside them are removed.
func (s *Service) ApplyRetention(ctx context.Context) error {
	st := s.LoadSettings(ctx)
	if !st.Retention.Enabled {
		return nil
	}
	rows, err := s.Pool.Query(ctx, `SELECT r.id, r.path FROM (
			SELECT x.id, x.path, x.finished_at,
				row_number() OVER (PARTITION BY x.series_rule_id ORDER BY x.start_at DESC) AS n,
				CASE WHEN x.series_rule_id IS NULL THEN 0 ELSE COALESCE(NULLIF(sr.keep_last, 0), $2) END AS keep
			FROM livetv_recordings x LEFT JOIN livetv_series_rules sr ON sr.id = x.series_rule_id
			WHERE x.status IN ('completed', 'cancelled') AND x.path <> '' AND x.size_bytes IS NOT NULL) r
		WHERE ($1 > 0 AND r.finished_at < now() - make_interval(days => $1)) OR (r.keep > 0 AND r.n > r.keep)`,
		st.Retention.DeleteAfterDays, st.Retention.KeepLast)
	if err != nil {
		return err
	}
	type victim struct {
		id   uuid.UUID
		path string
	}
	var vs []victim
	for rows.Next() {
		var v victim
		if err := rows.Scan(&v.id, &v.path); err != nil {
			rows.Close()
			return err
		}
		vs = append(vs, v)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	deleted := 0
	for _, v := range vs {
		p, err := s.recordingFile(v.path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			s.log().Warn("retention skipped a path outside the recordings folder", "path", v.path)
			continue
		}
		if err == nil {
			if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
				s.log().Warn("retention could not delete", "path", p, "err", err)
				continue
			}
			_ = os.Remove(strings.TrimSuffix(p, filepath.Ext(p)) + ".nfo")
			s.removeEmptyDirs(filepath.Dir(p))
		}
		if _, err := s.Pool.Exec(ctx, `UPDATE livetv_recordings SET status = 'deleted' WHERE id = $1`, v.id); err != nil {
			return err
		}
		deleted++
	}
	if deleted > 0 {
		s.log().Info("retention removed recordings", "count", deleted)
		s.scanLibrary(ctx)
	}
	return nil
}

// removeEmptyDirs removes now-empty folders up to (not including) the recordings folder.
func (s *Service) removeEmptyDirs(dir string) {
	root := filepath.Clean(s.RecordingsDir)
	for d := filepath.Clean(dir); strings.HasPrefix(d, root+string(filepath.Separator)); d = filepath.Dir(d) {
		entries, err := os.ReadDir(d)
		if err != nil {
			return
		}
		// A tvshow.nfo written by the DVR does not keep a folder alive.
		if len(entries) == 1 && entries[0].Name() == "tvshow.nfo" {
			_ = os.Remove(filepath.Join(d, "tvshow.nfo"))
		} else if len(entries) > 0 {
			return
		}
		if os.Remove(d) != nil {
			return
		}
	}
}
