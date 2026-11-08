package activity

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

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
