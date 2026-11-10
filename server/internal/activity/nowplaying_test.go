package activity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestWatchWakesOnCommand(t *testing.T) {
	r := &Registry{}
	r.Start(Session{Key: "s1", ItemID: uuid.New()})
	ch, stop := r.Watch("s1")
	if !r.Send("s1", Command{Kind: "message", Text: "hi"}) {
		t.Fatal("send failed")
	}
	select {
	case <-ch:
	default:
		t.Fatal("no wake-up after a queued command")
	}
	if got := r.Take("s1"); len(got) != 1 || got[0].Text != "hi" {
		t.Fatalf("take: %+v", got)
	}
	stop()
	r.Start(Session{Key: "s1", ItemID: uuid.New()})
	r.Send("s1", Command{Kind: "stop"})
	select {
	case <-ch:
		t.Fatal("woken after it stopped watching")
	default:
	}
	// A watcher that starts after a command was queued is woken at once.
	ch2, stop2 := r.Watch("s1")
	defer stop2()
	select {
	case <-ch2:
	default:
		t.Fatal("pending command not signalled to a new watcher")
	}
}

func TestRegistryTracksAndExpiresSessions(t *testing.T) {
	now := time.Unix(1000, 0)
	r := &Registry{TTL: time.Minute, Now: func() time.Time { return now }}
	movie, other := uuid.New(), uuid.New()

	if !r.Start(Session{Key: "s1", ItemID: movie, Title: "Dune"}) {
		t.Fatal("first start should be new")
	}
	now = now.Add(10 * time.Second)
	if r.Start(Session{Key: "s1", ItemID: movie, Title: "Dune"}) {
		t.Fatal("restarting the same item (e.g. audio switch) is not a new playback")
	}
	r.Progress("s1", movie, 5000)
	r.Progress("s1", other, 9999) // different item: ignored
	r.Progress("nope", movie, 1)  // unknown session: ignored
	got := r.Active()
	if len(got) != 1 || got[0].PositionMS != 5000 {
		t.Fatalf("active: %+v", got)
	}
	if !r.Start(Session{Key: "s1", ItemID: other}) {
		t.Fatal("another item is a new playback")
	}
	now = now.Add(2 * time.Minute)
	if len(r.Active()) != 0 {
		t.Fatal("stale session should expire")
	}
}

func TestRegistryCommands(t *testing.T) {
	now := time.Unix(1000, 0)
	r := &Registry{Now: func() time.Time { return now }}
	if r.Send("s1", Command{Kind: "message", Text: "hi"}) {
		t.Fatal("no session yet")
	}
	r.Start(Session{Key: "s1", ItemID: uuid.New()})
	if !r.Send("s1", Command{Kind: "message", Text: "Dinner!"}) || !r.Send("s1", Command{Kind: "stop"}) {
		t.Fatal("send to a live session")
	}
	if len(r.Active()) != 0 {
		t.Fatal("stop ends the session on the dashboard")
	}
	got := r.Take("s1")
	if len(got) != 2 || got[0].Text != "Dinner!" || got[1].Kind != "stop" {
		t.Fatalf("take: %+v", got)
	}
	if len(r.Take("s1")) != 0 {
		t.Fatal("commands are taken once")
	}
}
