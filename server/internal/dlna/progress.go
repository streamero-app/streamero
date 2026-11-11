package dlna

import (
	"context"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/activity"
)

// DLNA renderers do not report playback, so progress is estimated from what
// they fetch: the byte offset reached in a file (a TV reads a little ahead), or
// the start of a converted stream plus the time it has been streaming. When a
// TV stops fetching for idleAfter, the estimate is saved as the viewer's
// position (the usual resume thresholds then apply: too early is dropped, past
// 90% counts as watched). Samsung TVs that send X_SetBookmark override it.
const idleAfter = 60 * time.Second

type tracker struct {
	mu    sync.Mutex
	plays map[string]*play
	now   func() time.Time
}

type play struct {
	t      *tracker
	reg    *activity.Registry
	key    string
	regKey string
	owner  uuid.UUID
	item   uuid.UUID
	dur    int64
	pos    int64
	last   time.Time
}

func (t *tracker) clock() time.Time {
	if t.now != nil {
		return t.now()
	}
	return time.Now()
}

// start registers (or continues) the playback of item by the requesting TV.
func (t *tracker) start(s *Server, r *http.Request, item uuid.UUID, dur int64, device, method string) *play {
	_, v := s.Current(r.Context())
	if v == nil {
		return &play{}
	}
	ip := clientIP(r)
	key := ip + "|" + item.String()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.plays == nil {
		t.plays = map[string]*play{}
	}
	if p, ok := t.plays[key]; ok {
		p.last = t.clock()
		return p
	}
	p := &play{t: t, reg: s.NowPlaying, key: key, regKey: "dlna:" + ip, owner: v.Owner, item: item, dur: dur, last: t.clock()}
	t.plays[key] = p
	if s.NowPlaying != nil {
		title := ""
		_ = s.Pool.QueryRow(r.Context(), `SELECT COALESCE(metadata_title, title) FROM items WHERE id = $1`, item).Scan(&title)
		sess := activity.Session{Key: p.regKey, User: v.Principal.User.Username, Device: device + " (" + ip + ")", Client: "DLNA",
			ItemID: item, Title: title, Method: method, DurationMS: dur}
		if v.Principal.Profile != nil {
			sess.Profile = v.Principal.Profile.Name
		}
		s.NowPlaying.Start(sess)
	}
	return p
}

// at records the estimated position.
func (p *play) at(ms int64) {
	if p.t == nil {
		return
	}
	p.t.mu.Lock()
	p.pos, p.last = ms, p.t.clock()
	p.t.mu.Unlock()
	if p.reg != nil {
		p.reg.Progress(p.regKey, p.item, ms)
	}
}

// forget drops a playback whose position was reported explicitly.
func (t *tracker) forget(ip string, item uuid.UUID) {
	t.mu.Lock()
	delete(t.plays, ip+"|"+item.String())
	t.mu.Unlock()
}

// idle removes the playbacks without requests for idleAfter and returns copies.
func (t *tracker) idle() []play {
	t.mu.Lock()
	defer t.mu.Unlock()
	var out []play
	now := t.clock()
	for k, p := range t.plays {
		if now.Sub(p.last) >= idleAfter {
			out = append(out, *p)
			delete(t.plays, k)
		}
	}
	return out
}

// reap saves the positions of finished playbacks until ctx ends.
func (t *tracker) reap(ctx context.Context, s *Server) {
	tick := time.NewTicker(15 * time.Second)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			for _, p := range t.idle() {
				if p.pos <= 0 {
					continue
				}
				dur := p.dur
				if err := s.Library.SaveProgress(ctx, p.owner, p.item, p.pos, &dur, false); err != nil && s.Log != nil {
					s.Log.Debug("dlna: saving progress failed", "err", err)
				}
			}
		}
	}
}
