package logbuf

import (
	"context"
	"io"
	"log/slog"
	"testing"
)

func TestBufferKeepsFiltersAndOrders(t *testing.T) {
	b := New("serve")
	log := slog.New(b.Handler(slog.NewTextHandler(io.Discard, &slog.HandlerOptions{Level: slog.LevelDebug})))
	log.Debug("noise")
	log.Info("library scanned", "files", 12)
	log.With("job", "scan").Warn("slow disk")
	log.Error("ffmpeg failed", "code", 1)
	got := b.Recent(context.Background(), "info", "", 10)
	if len(got) != 3 || got[0].Message != "ffmpeg failed" || got[2].Message != "library scanned" {
		t.Fatalf("order/level: %+v", got)
	}
	if got[1].Attrs != "job=scan" || got[2].Attrs != "files=12" || got[0].Source != "serve" {
		t.Fatalf("attrs: %+v", got)
	}
	if q := b.Recent(context.Background(), "debug", "DISK", 10); len(q) != 1 || q[0].Level != "warn" {
		t.Fatalf("search: %+v", q)
	}
	if l := b.Recent(context.Background(), "debug", "", 2); len(l) != 2 {
		t.Fatalf("limit: %d", len(l))
	}
}

func TestRingWraps(t *testing.T) {
	b := New("worker")
	log := slog.New(b.Handler(slog.NewTextHandler(io.Discard, nil)))
	for i := 0; i < 1500; i++ {
		log.Info("tick", "i", i)
	}
	got := b.Recent(context.Background(), "info", "", 2000)
	if len(got) != 1000 || got[0].Attrs != "i=1499" {
		t.Fatalf("ring: %d %+v", len(got), got[0])
	}
}
