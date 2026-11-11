package syncplay

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

type clock struct{ t time.Time }

func (c *clock) now() time.Time          { return c.t }
func (c *clock) advance(d time.Duration) { c.t = c.t.Add(d) }
func newTestHub() (*Hub, *clock) {
	c := &clock{t: time.Date(2026, 11, 11, 20, 0, 0, 0, time.UTC)}
	h := NewHub()
	h.Now = c.now
	return h, c
}

func ident(name string) Identity {
	return Identity{Key: "s-" + name, UserID: uuid.New(), Name: name, Device: "Test"}
}

func entries(n int) []Entry {
	out := make([]Entry, n)
	for i := range out {
		out[i] = Entry{ID: uuid.New(), ItemID: uuid.New(), Title: "Film", DurationMS: 3_600_000}
	}
	return out
}

// drain returns the last group state delivered on ch (and any correction).
func drain(ch <-chan Event) (last *Snapshot, corr *Correction, gone string) {
	for {
		select {
		case e := <-ch:
			if e.Correction != nil {
				corr = e.Correction
			} else if e.Group != nil {
				last = e.Group
			} else {
				gone = e.Reason
			}
		default:
			return
		}
	}
}

func mustGroup(t *testing.T, h *Hub, key string) Snapshot {
	t.Helper()
	g, ok := h.GroupOf(key)
	if !ok {
		t.Fatalf("%s is in no group", key)
	}
	return g
}

func memberOf(g Snapshot, name string) MemberView {
	for _, m := range g.Members {
		if m.Name == name {
			return m
		}
	}
	return MemberView{}
}

func TestDrift(t *testing.T) {
	for _, tc := range []struct {
		drift int64
		speed float64
		mode  string
		rate  float64
		dur   int64
	}{
		{0, 1, "", 0, 0},
		{ToleranceMS, 1, "", 0, 0},
		{-ToleranceMS, 1, "", 0, 0},
		{-400, 1, "rate", 1.05, 8000}, // behind: faster, 400 / 0.05
		{400, 1, "rate", 0.95, 8000},  // ahead: slower
		{-1000, 1, "rate", 1.10, 10000},
		{-1000, 2, "rate", 2.2, 5000}, // relative to the group speed
		{SeekThresholdMS + 1, 1, "seek", 0, 0},
		{-10_000, 1, "seek", 0, 0},
	} {
		c := Drift(tc.drift, tc.speed)
		if tc.mode == "" {
			if c != nil {
				t.Errorf("drift %d: want none, got %+v", tc.drift, c)
			}
			continue
		}
		if c == nil || c.Mode != tc.mode || c.DriftMS != tc.drift {
			t.Errorf("drift %d: got %+v, want %s", tc.drift, c, tc.mode)
			continue
		}
		if tc.mode == "rate" && (abs(c.Rate-tc.rate) > 1e-9 || c.DurationMS != tc.dur) {
			t.Errorf("drift %d speed %v: rate %v for %d ms, want %v for %d", tc.drift, tc.speed, c.Rate, c.DurationMS, tc.rate, tc.dur)
		}
	}
}

func abs(f float64) float64 {
	if f < 0 {
		return -f
	}
	return f
}

func TestStartWaitsForEveryConnectedMember(t *testing.T) {
	h, c := newTestHub()
	ana, ben := ident("Ana"), ident("Ben")
	q := entries(2)
	g, err := h.Create(ana, "", q, 0, 5000)
	if err != nil {
		t.Fatal(err)
	}
	if g.State != Waiting || g.Name != "Ana" || g.SelfID == nil || g.PositionMS != 5000 {
		t.Fatalf("created: %+v", g)
	}
	aCh, aOff := h.Subscribe(ana.Key)
	defer aOff()
	if _, err := h.Join(g.ID, ben); err != nil {
		t.Fatal(err)
	}
	bCh, bOff := h.Subscribe(ben.Key)
	defer bOff()

	// Ana is ready; Ben (connected) is not: still waiting, and Ben is who it waits for.
	if err := h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID}); err != nil {
		t.Fatal(err)
	}
	s, _, _ := drain(aCh)
	if s.State != Waiting || !memberOf(*s, "Ana").Ready || memberOf(*s, "Ben").Ready || !memberOf(*s, "Ben").Connected {
		t.Fatalf("waiting for Ben: %+v", s)
	}
	// A report about another entry is stale.
	_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: q[1].ID})
	if mustGroup(t, h, ana.Key).State != Waiting {
		t.Fatal("stale ready started the group")
	}
	_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: q[0].ID})
	s, _, _ = drain(bCh)
	if s.State != Playing || s.AnchorAt != c.t.Add(h.StartDelay).UnixMilli() || s.Reason != "ready" {
		t.Fatalf("start: %+v", s)
	}
	// The start is scheduled: the position holds until the anchor, then runs.
	if p := s.PositionAt(c.t); p != 5000 {
		t.Fatalf("position before the anchor: %d", p)
	}
	if p := s.PositionAt(c.t.Add(h.StartDelay + 2*time.Second)); p != 7000 {
		t.Fatalf("position 2 s after the anchor: %d", p)
	}
}

func TestDisconnectedMembersNeverBlock(t *testing.T) {
	h, c := newTestHub()
	ana, ben := ident("Ana"), ident("Ben")
	q := entries(1)
	g, _ := h.Create(ana, "Movie night", q, 0, 0)
	_, aOff := h.Subscribe(ana.Key)
	defer aOff()
	_, _ = h.Join(g.ID, ben) // Ben never connects
	_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID})
	if s := mustGroup(t, h, ana.Key); s.State != Playing {
		t.Fatalf("a member without a connection held up the start: %s", s.State)
	}
	// Ben stays a member for MemberTimeout, then is dropped.
	c.advance(h.MemberTimeout - time.Second)
	h.Tick()
	if len(mustGroup(t, h, ana.Key).Members) != 2 {
		t.Fatal("dropped too early")
	}
	c.advance(2 * time.Second)
	h.Tick()
	s := mustGroup(t, h, ana.Key)
	if len(s.Members) != 1 || s.State != Playing {
		t.Fatalf("after timeout: %+v", s)
	}
	if _, ok := h.GroupOf(ben.Key); ok {
		t.Fatal("Ben still in the group")
	}
}

func TestReconnectKeepsMembership(t *testing.T) {
	h, c := newTestHub()
	ana := ident("Ana")
	q := entries(1)
	g, _ := h.Create(ana, "", q, 0, 0)
	_, off := h.Subscribe(ana.Key)
	_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID})
	off() // network drop
	c.advance(30 * time.Second)
	h.Tick()
	ch, off2 := h.Subscribe(ana.Key)
	defer off2()
	s, _, _ := drain(ch)
	if s == nil || s.ID != g.ID || s.State != Playing || memberOf(*s, "Ana").Ready {
		t.Fatalf("reconnect: %+v", s)
	}
	if s.PositionAt(c.t) < 29_000 {
		t.Fatalf("playback did not go on while disconnected: %d", s.PositionAt(c.t))
	}
}

func TestBufferingPausesEveryoneUntilReady(t *testing.T) {
	h, c := newTestHub()
	ana, ben := ident("Ana"), ident("Ben")
	q := entries(1)
	g, _ := h.Create(ana, "", q, 0, 0)
	aCh, aOff := h.Subscribe(ana.Key)
	defer aOff()
	_, _ = h.Join(g.ID, ben)
	_, bOff := h.Subscribe(ben.Key)
	defer bOff()
	_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID})
	_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: q[0].ID})
	c.advance(h.StartDelay + 10*time.Second)
	_ = h.Report(ben.Key, Report{Kind: "buffering", EntryID: q[0].ID})
	s, _, _ := drain(aCh)
	if s.State != Waiting || s.PositionMS != 10_000 || !memberOf(*s, "Ben").Buffering || s.Actor != "Ben" || s.Reason != "buffering" {
		t.Fatalf("buffering: %+v", s)
	}
	c.advance(3 * time.Second)
	if p := s.PositionAt(c.t); p != 10_000 {
		t.Fatalf("position moved while waiting: %d", p)
	}
	_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: q[0].ID})
	s, _, _ = drain(aCh)
	if s.State != Playing || s.PositionMS != 10_000 || memberOf(*s, "Ben").Buffering {
		t.Fatalf("resume: %+v", s)
	}
}

func TestWaitTimeoutGoesOnWithoutLaggards(t *testing.T) {
	h, c := newTestHub()
	ana, ben := ident("Ana"), ident("Ben")
	q := entries(1)
	g, _ := h.Create(ana, "", q, 0, 0)
	_, aOff := h.Subscribe(ana.Key)
	defer aOff()
	_, _ = h.Join(g.ID, ben)
	_, bOff := h.Subscribe(ben.Key)
	defer bOff()
	_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID})
	c.advance(h.WaitTimeout + time.Second)
	h.Tick()
	s := mustGroup(t, h, ana.Key)
	if s.State != Playing || !memberOf(s, "Ben").Lagging || s.Reason != "timeout" {
		t.Fatalf("timeout: %+v", s)
	}
	// Ben catches up later: ready clears lagging without stopping anyone.
	_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: q[0].ID})
	s = mustGroup(t, h, ana.Key)
	if s.State != Playing || memberOf(s, "Ben").Lagging || !memberOf(s, "Ben").Ready {
		t.Fatalf("catch up: %+v", s)
	}
}

func TestCommands(t *testing.T) {
	h, c := newTestHub()
	ana, ben := ident("Ana"), ident("Ben")
	q := entries(3)
	g, _ := h.Create(ana, "", q, 0, 0)
	_, aOff := h.Subscribe(ana.Key)
	defer aOff()
	_, _ = h.Join(g.ID, ben)
	_, bOff := h.Subscribe(ben.Key)
	defer bOff()
	ready := func() {
		s := mustGroup(t, h, ana.Key)
		cur, _ := s.Current()
		_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: cur.ID})
		_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: cur.ID})
	}
	ready()
	c.advance(h.StartDelay + 4*time.Second)

	if err := h.Do(ben.Key, Command{Kind: "pause"}); err != nil {
		t.Fatal(err)
	}
	s := mustGroup(t, h, ana.Key)
	if s.State != Paused || s.PositionMS != 4000 || s.Actor != "Ben" {
		t.Fatalf("pause: %+v", s)
	}
	// Play from pause: everyone is still ready, so it starts right away (scheduled).
	_ = h.Do(ana.Key, Command{Kind: "play"})
	if s = mustGroup(t, h, ana.Key); s.State != Playing || s.AnchorAt != c.t.Add(h.StartDelay).UnixMilli() {
		t.Fatalf("play: %+v", s)
	}
	// Seek while playing: everyone has to get ready at the new position.
	c.advance(time.Second)
	_ = h.Do(ana.Key, Command{Kind: "seek", PositionMS: 60_000})
	if s = mustGroup(t, h, ana.Key); s.State != Waiting || s.PositionMS != 60_000 || memberOf(s, "Ana").Ready {
		t.Fatalf("seek: %+v", s)
	}
	ready()
	if s = mustGroup(t, h, ana.Key); s.State != Playing || s.PositionMS != 60_000 {
		t.Fatalf("after seek: %+v", s)
	}
	// Speed rebases the position and keeps playing.
	c.advance(h.StartDelay + 2*time.Second)
	_ = h.Do(ben.Key, Command{Kind: "speed", Speed: 1.5})
	s = mustGroup(t, h, ana.Key)
	if s.State != Playing || s.Speed != 1.5 || s.PositionMS != 62_000 {
		t.Fatalf("speed: %+v", s)
	}
	if p := s.PositionAt(c.t.Add(2 * time.Second)); p != 65_000 {
		t.Fatalf("position at 1.5x: %d", p)
	}
	if err := h.Do(ben.Key, Command{Kind: "speed", Speed: 10}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("speed 10: %v", err)
	}
	// Two members reach the end: the queue moves on once.
	_ = h.Do(ana.Key, Command{Kind: "next", EntryID: q[0].ID})
	_ = h.Do(ben.Key, Command{Kind: "next", EntryID: q[0].ID})
	if s = mustGroup(t, h, ana.Key); s.Index != 1 || s.PositionMS != 0 || s.State != Waiting {
		t.Fatalf("next: %+v", s)
	}
	_ = h.Do(ana.Key, Command{Kind: "jump", EntryID: q[2].ID})
	_ = h.Do(ana.Key, Command{Kind: "next", EntryID: q[2].ID})
	if s = mustGroup(t, h, ana.Key); s.State != Idle || s.Reason != "ended" {
		t.Fatalf("end of queue: %+v", s)
	}
	// A new queue starts again.
	_ = h.Do(ben.Key, Command{Kind: "setQueue", Entries: entries(1), PositionMS: 1000})
	if s = mustGroup(t, h, ana.Key); s.State != Waiting || s.Index != 0 || s.PositionMS != 1000 || len(s.Queue) != 1 {
		t.Fatalf("set queue: %+v", s)
	}
	if err := h.Do("nobody", Command{Kind: "play"}); !errors.Is(err, ErrNotInGroup) {
		t.Fatalf("outsider: %v", err)
	}
}

func TestLeavingStopsNothingForOthers(t *testing.T) {
	h, c := newTestHub()
	ana, ben := ident("Ana"), ident("Ben")
	q := entries(1)
	g, _ := h.Create(ana, "", q, 0, 0)
	_, aOff := h.Subscribe(ana.Key)
	defer aOff()
	_, _ = h.Join(g.ID, ben)
	bCh, bOff := h.Subscribe(ben.Key)
	defer bOff()
	_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID})
	_ = h.Report(ben.Key, Report{Kind: "ready", EntryID: q[0].ID})
	c.advance(5 * time.Second)
	_ = h.Report(ben.Key, Report{Kind: "buffering", EntryID: q[0].ID})
	// Ben leaves while everyone waits for him: the others play on.
	if !h.Leave(ben.Key) {
		t.Fatal("leave")
	}
	if _, _, gone := drain(bCh); gone != "left" {
		t.Fatalf("leaver told: %q", gone)
	}
	s := mustGroup(t, h, ana.Key)
	if s.State != Playing || len(s.Members) != 1 {
		t.Fatalf("after leave: %+v", s)
	}
	// The last member leaving closes the group.
	h.Leave(ana.Key)
	if _, ok := h.Group(g.ID); ok || len(h.List()) != 0 {
		t.Fatal("empty group kept")
	}
	// Joining another group leaves the first.
	g1, _ := h.Create(ana, "one", nil, 0, 0)
	g2, _ := h.Create(ben, "two", nil, 0, 0)
	if _, err := h.Join(g2.ID, ana); err != nil {
		t.Fatal(err)
	}
	if _, ok := h.Group(g1.ID); ok {
		t.Fatal("first group not closed when its only member moved")
	}
	if _, err := h.Join(uuid.New(), ana); !errors.Is(err, ErrNotFound) {
		t.Fatalf("unknown group: %v", err)
	}
}

func TestDriftCorrectionThroughReports(t *testing.T) {
	h, c := newTestHub()
	ana := ident("Ana")
	tv := ident("TV")
	tv.Compat = true
	q := entries(1)
	g, _ := h.Create(ana, "", q, 0, 0)
	ch, off := h.Subscribe(ana.Key)
	defer off()
	_, _ = h.Join(g.ID, tv)
	tvCh, tvOff := h.Subscribe(tv.Key)
	defer tvOff()
	_ = h.Report(ana.Key, Report{Kind: "ready", EntryID: q[0].ID})
	_ = h.Report(tv.Key, Report{Kind: "ready", EntryID: q[0].ID})
	c.advance(h.StartDelay + 10*time.Second)
	drain(ch)
	drain(tvCh)

	// In sync: nothing.
	_ = h.Report(ana.Key, Report{Kind: "position", EntryID: q[0].ID, PositionMS: 10_050, At: c.t, Playing: true})
	if _, corr, _ := drain(ch); corr != nil {
		t.Fatalf("in sync but corrected: %+v", corr)
	}
	// 500 ms behind (reported a second ago): speed up.
	_ = h.Report(ana.Key, Report{Kind: "position", EntryID: q[0].ID, PositionMS: 8_500, At: c.t.Add(-time.Second), Playing: true})
	_, corr, _ := drain(ch)
	if corr == nil || corr.Mode != "rate" || corr.Rate <= 1 || corr.DriftMS != -500 {
		t.Fatalf("behind: %+v", corr)
	}
	// While the correction runs, further reports are not judged.
	c.advance(time.Second)
	_ = h.Report(ana.Key, Report{Kind: "position", EntryID: q[0].ID, PositionMS: 5_000, At: c.t, Playing: true})
	if _, corr, _ := drain(ch); corr != nil {
		t.Fatalf("corrected during a correction: %+v", corr)
	}
	// Far off after the correction window: seek to the shared position.
	c.advance(time.Duration(10_000) * time.Millisecond)
	_ = h.Report(ana.Key, Report{Kind: "position", EntryID: q[0].ID, PositionMS: 1_000, At: c.t, Playing: true})
	_, corr, _ = drain(ch)
	if corr == nil || corr.Mode != "seek" || corr.PositionMS != 21_000 {
		t.Fatalf("far behind: %+v", corr)
	}
	// Compat clients correct drift themselves.
	_ = h.Report(tv.Key, Report{Kind: "position", EntryID: q[0].ID, PositionMS: 1_000, At: c.t, Playing: true})
	if _, corr, _ := drain(tvCh); corr != nil {
		t.Fatalf("compat member corrected: %+v", corr)
	}
}

func TestRateLimits(t *testing.T) {
	h, _ := newTestHub()
	ana := ident("Ana")
	q := entries(1)
	_, _ = h.Create(ana, "", q, 0, 0)
	limited := false
	for range 50 {
		if err := h.Do(ana.Key, Command{Kind: "seek", PositionMS: 1000}); errors.Is(err, ErrRateLimited) {
			limited = true
			break
		}
	}
	if !limited {
		t.Fatal("commands were not rate limited")
	}
	// Creating groups counts against the same budget.
	if _, err := h.Create(ana, "", nil, 0, 0); !errors.Is(err, ErrRateLimited) {
		t.Fatalf("create while limited: %v", err)
	}
	h.MaxGroups = 1
	if _, err := h.Create(ident("Ben"), "", nil, 0, 0); !errors.Is(err, ErrTooManyGroups) {
		t.Fatalf("group cap: %v", err)
	}
}
