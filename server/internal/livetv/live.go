package livetv

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os/exec"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// lease is one use of a tuner: a live session or a recording.
type lease struct {
	tuner     uuid.UUID
	recording bool
	evict     func() // stops a live session so a recording can take its tuner
}

// acquire takes a tuner of a source. capacity 0 = unlimited. A recording may
// take the tuner of a live session (recordings win over viewers).
func (s *Service) acquire(tuner uuid.UUID, capacity int, recording bool, evict func()) (*lease, error) {
	s.mu.Lock()
	if s.leases == nil {
		s.leases = map[uuid.UUID][]*lease{}
	}
	l := &lease{tuner: tuner, recording: recording, evict: evict}
	if capacity <= 0 || len(s.leases[tuner]) < capacity {
		s.leases[tuner] = append(s.leases[tuner], l)
		s.mu.Unlock()
		return l, nil
	}
	var victim *lease
	if recording {
		for _, x := range s.leases[tuner] {
			if !x.recording && x.evict != nil {
				victim = x
				break
			}
		}
	}
	s.mu.Unlock()
	if victim == nil {
		return nil, ErrTunersBusy
	}
	victim.evict() // releases its lease through the session's stop hook
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.leases[tuner]) >= capacity {
		return nil, ErrTunersBusy
	}
	s.leases[tuner] = append(s.leases[tuner], l)
	return l, nil
}

func (s *Service) release(l *lease) {
	if l == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	list := s.leases[l.tuner]
	for i, x := range list {
		if x == l {
			s.leases[l.tuner] = append(list[:i:i], list[i+1:]...)
			return
		}
	}
}

// InUse reports how many tuners of a source are taken.
func (s *Service) InUse(tuner uuid.UUID) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.leases[tuner])
}

type liveSession struct {
	hlsID   string
	channel uuid.UUID
	tuner   uuid.UUID
	plan    stream.Plan
}

// streamProtocols are the protocols ffmpeg may use for a channel (no local
// files or special protocols, whatever a playlist says).
const streamProtocols = "http,https,tcp,tls,crypto,udp,rtp,rtsp,rtsps,rtmp,rtmps,httpproxy"

// inputArgs are the ffmpeg input options for a channel URL.
func inputArgs(raw, userAgent string) []string {
	a := []string{"-protocol_whitelist", streamProtocols, "-fflags", "+genpts+discardcorrupt"}
	if u, err := url.Parse(raw); err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		a = append(a, "-reconnect", "1", "-reconnect_streamed", "1", "-reconnect_delay_max", "5", "-user_agent", userAgentOr(userAgent))
	}
	return a
}

// Playback says how to play a channel.
type Playback struct {
	URL              string
	Method           string
	Reason           string
	TimeshiftSeconds int
	Now              *Program
}

func (s *Service) tunerCapacity(ctx context.Context, tuner uuid.UUID) int {
	var n int
	_ = s.Pool.QueryRow(ctx, `SELECT tuner_count FROM livetv_tuners WHERE id = $1`, tuner).Scan(&n)
	return n
}

func (s *Service) segmentSeconds(ctx context.Context) int {
	if s.HLS != nil && s.HLS.Encoding != nil {
		return s.HLS.Encoding(ctx).Normalize().SegmentSeconds
	}
	return stream.DefaultEncoding().SegmentSeconds
}

// Play tunes a channel for a viewer: viewers with the same delivery share one
// session (and tuner); otherwise a tuner is taken and ffmpeg writes a sliding
// HLS window (time-shift buffer).
func (s *Service) Play(ctx context.Context, p auth.Principal, channelID uuid.UUID, dev stream.Device) (Playback, error) {
	owner := stream.Owner(p)
	ch, err := s.ChannelByID(ctx, owner, channelID)
	if err != nil {
		return Playback{}, err
	}
	capacity := s.tunerCapacity(ctx, ch.TunerID)
	settings := s.LoadSettings(ctx)
	segs := s.segmentSeconds(ctx)
	window := max(settings.TimeshiftMinutes*60/max(segs, 1), 3)

	plan := s.livePlan(ch, dev)
	if err := allowed(p, plan); err != nil && ch.VideoCodec != "" {
		return Playback{}, err
	}
	key := channelID.String() + "|" + planKey(plan)
	s.mu.Lock()
	if s.live == nil {
		s.live = map[string]*liveSession{}
	}
	ls := s.live[key]
	s.mu.Unlock()
	if ls != nil && s.HLS.Alive(ls.hlsID) {
		return s.playback(ls.hlsID, plan, window*segs, ch), nil
	}
	if ls != nil {
		s.HLS.Stop(ls.hlsID)
	}

	var sess *liveSession
	l, err := s.acquire(ch.TunerID, capacity, false, func() {
		s.mu.Lock()
		x := sess
		s.mu.Unlock()
		if x != nil {
			s.HLS.Stop(x.hlsID)
		}
	})
	if err != nil {
		return Playback{}, err
	}
	if ch.VideoCodec == "" && ch.AudioCodec == "" {
		// M3U channels do not say what they carry: probe once and remember.
		if err := s.probeChannel(ctx, &ch); err != nil {
			s.release(l)
			s.log().Warn("live channel probe failed", "channel", ch.Name, "err", err)
			return Playback{}, ErrChannelUnavailable
		}
		plan = s.livePlan(ch, dev)
		if err := allowed(p, plan); err != nil {
			s.release(l)
			return Playback{}, err
		}
		key = channelID.String() + "|" + planKey(plan)
	}
	audio := 0
	if ch.AudioCodec == "" {
		audio = -1
	}
	job := stream.Job{Input: ch.StreamURL, AudioIndex: audio, Plan: plan, InputArgs: inputArgs(ch.StreamURL, ch.TunerUserAgent)}
	var started *liveSession
	id, err := s.HLS.StartLive(ctx, job, window, func() {
		s.release(l)
		s.mu.Lock()
		if started != nil && s.live[key] == started {
			delete(s.live, key)
		}
		s.mu.Unlock()
	})
	if err != nil {
		s.log().Warn("live channel failed to start", "channel", ch.Name, "err", err)
		if strings.Contains(err.Error(), "503") {
			return Playback{}, ErrTunersBusy // the device itself has no free tuner
		}
		return Playback{}, fmt.Errorf("%w: %w", ErrChannelUnavailable, err)
	}
	s.mu.Lock()
	started = &liveSession{hlsID: id, channel: channelID, tuner: ch.TunerID, plan: plan}
	sess = started
	s.live[key] = started
	s.mu.Unlock()
	return s.playback(id, plan, window*segs, ch), nil
}

func (s *Service) playback(hlsID string, plan stream.Plan, buffer int, ch Channel) Playback {
	scope := "/stream/hls/" + hlsID + "/"
	return Playback{URL: scope + "index.m3u8?" + s.Signer.Query(scope, stream.URLTTL), Method: plan.Method, Reason: plan.Reason,
		TimeshiftSeconds: buffer, Now: ch.Now}
}

// livePlan reuses the on-demand decision for the channel's codecs. Live TV is
// always HLS, so "direct" becomes a remux (stream copy into HLS).
func (s *Service) livePlan(ch Channel, dev stream.Device) stream.Plan {
	f := library.MediaFile{Container: "mpegts", VideoCodec: ch.VideoCodec, Height: ch.Height}
	audio := -1
	if ch.AudioCodec != "" {
		f.Audio = []library.AudioStream{{Codec: ch.AudioCodec, Channels: 2}}
		audio = 0
	}
	if f.VideoCodec == "" {
		f.VideoCodec = "unknown"
	}
	dev.Containers = append(append([]string(nil), dev.Containers...), "mpegts")
	plan := stream.Decide(f, audio, dev)
	if plan.Method == "direct" {
		plan.Method = "remux"
	}
	if plan.TranscodeVideo {
		plan.Deinterlace = true
		plan.ToneMap = false
	}
	return plan
}

func planKey(p stream.Plan) string {
	return fmt.Sprintf("%s:%t:%t:%d:%d", p.Method, p.TranscodeVideo, p.TranscodeAudio, p.Height, p.MaxBitrate)
}

// allowed applies the account's playback permissions to a live plan.
func allowed(p auth.Principal, plan stream.Plan) error {
	if p.User.IsAdmin {
		return nil
	}
	perms := p.User.Permissions
	if (plan.TranscodeVideo && !perms.VideoTranscoding) || (plan.TranscodeAudio && !perms.AudioTranscoding) ||
		(plan.Method == "remux" && !perms.Remux) {
		return stream.ErrTranscodeDenied
	}
	return nil
}

type probeOut struct {
	Streams []struct {
		CodecType string `json:"codec_type"`
		CodecName string `json:"codec_name"`
		Height    int    `json:"height"`
	} `json:"streams"`
}

// probeChannel reads the codecs of a channel with ffprobe and stores them.
func (s *Service) probeChannel(ctx context.Context, ch *Channel) error {
	bin := s.FFprobe
	if bin == "" {
		bin = "ffprobe"
	}
	pctx, cancel := context.WithTimeout(ctx, 25*time.Second)
	defer cancel()
	args := append([]string{"-v", "error"}, inputArgs(ch.StreamURL, ch.TunerUserAgent)...)
	args = append(args, "-analyzeduration", "5000000", "-probesize", "5000000", "-show_streams", "-of", "json", "-i", ch.StreamURL)
	out, err := exec.CommandContext(pctx, bin, args...).Output() // #nosec G204 -- fixed binary; URL scheme checked, protocols whitelisted
	if err != nil {
		return err
	}
	var po probeOut
	if err := json.Unmarshal(out, &po); err != nil {
		return err
	}
	for _, st := range po.Streams {
		switch st.CodecType {
		case "video":
			if ch.VideoCodec == "" && st.CodecName != "" {
				ch.VideoCodec = st.CodecName
				if st.Height > 0 {
					h := st.Height
					ch.Height = &h
				}
			}
		case "audio":
			if ch.AudioCodec == "" && st.CodecName != "" {
				ch.AudioCodec = st.CodecName
			}
		}
	}
	if ch.VideoCodec == "" && ch.AudioCodec == "" {
		return fmt.Errorf("no audio or video in the stream")
	}
	_, err = s.Pool.Exec(ctx, `UPDATE livetv_channels SET video_codec = $2, audio_codec = $3, height = $4, probed_at = now() WHERE id = $1`,
		ch.ID, ch.VideoCodec, ch.AudioCodec, ch.Height)
	return err
}

// stopTunerSessions stops every live session on a tuner source.
func (s *Service) stopTunerSessions(tuner uuid.UUID) {
	s.mu.Lock()
	var ids []string
	for _, ls := range s.live {
		if ls.tuner == tuner {
			ids = append(ids, ls.hlsID)
		}
	}
	s.mu.Unlock()
	for _, id := range ids {
		s.HLS.Stop(id)
	}
}

// ActiveSessions is the number of live sessions (tests, dashboard).
func (s *Service) ActiveSessions() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.live)
}
