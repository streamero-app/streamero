// Package logbuf keeps the recent log records of the server and the worker so
// administrators can read them in the web client. Records go to a shared Valkey
// list (both processes write to it) and to a small in-memory ring as a fallback.
package logbuf

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

// Key is the Valkey list holding the newest records first.
const Key = "streamero:logs"

// Keep is how many records the shared list holds.
const Keep = 5000

// Entry is one log record.
type Entry struct {
	Time    time.Time `json:"time"`
	Level   string    `json:"level"`
	Message string    `json:"message"`
	Source  string    `json:"source"`
	Attrs   string    `json:"attrs,omitempty"`
}

// Buffer collects records; attach Valkey with Attach once it is reachable.
type Buffer struct {
	Source string
	mu     sync.Mutex
	ring   []Entry
	next   int
	full   bool
	vk     *redis.Client
	queue  chan Entry
}

// New returns a Buffer for one process ("serve" or "worker").
func New(source string) *Buffer {
	return &Buffer{Source: source, ring: make([]Entry, 1000)}
}

// Attach starts copying records to the shared list until ctx ends.
func (b *Buffer) Attach(ctx context.Context, vk *redis.Client) {
	b.mu.Lock()
	if b.queue != nil {
		b.mu.Unlock()
		return
	}
	b.vk = vk
	b.queue = make(chan Entry, 512)
	b.mu.Unlock()
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case e := <-b.queue:
				raw, _ := json.Marshal(e)
				pipe := vk.Pipeline()
				pipe.LPush(ctx, Key, raw)
				pipe.LTrim(ctx, Key, 0, Keep-1)
				_, _ = pipe.Exec(ctx)
			}
		}
	}()
}

func (b *Buffer) add(e Entry) {
	b.mu.Lock()
	b.ring[b.next] = e
	b.next = (b.next + 1) % len(b.ring)
	if b.next == 0 {
		b.full = true
	}
	q := b.queue
	b.mu.Unlock()
	if q != nil {
		select {
		case q <- e:
		default: // never block logging on Valkey
		}
	}
}

var order = map[string]int{"debug": 0, "info": 1, "warn": 2, "error": 3}

// Recent returns up to limit records at or above level, newest first, whose
// message or fields contain q (case-insensitive).
func (b *Buffer) Recent(ctx context.Context, level, q string, limit int) []Entry {
	var all []Entry
	b.mu.Lock()
	vk := b.vk
	b.mu.Unlock()
	if vk != nil {
		if raws, err := vk.LRange(ctx, Key, 0, Keep-1).Result(); err == nil {
			for _, r := range raws {
				var e Entry
				if json.Unmarshal([]byte(r), &e) == nil {
					all = append(all, e)
				}
			}
		}
	}
	if all == nil {
		b.mu.Lock()
		n := b.next
		if b.full {
			n = len(b.ring)
		}
		for i := 0; i < n; i++ {
			all = append(all, b.ring[(b.next-1-i+len(b.ring))%len(b.ring)])
		}
		b.mu.Unlock()
	}
	min := order[level]
	q = strings.ToLower(strings.TrimSpace(q))
	out := []Entry{}
	for _, e := range all {
		if order[e.Level] < min {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(e.Message+" "+e.Attrs), q) {
			continue
		}
		out = append(out, e)
		if len(out) >= limit {
			break
		}
	}
	return out
}

// Handler wraps another slog.Handler and copies every record it lets through.
func (b *Buffer) Handler(inner slog.Handler) slog.Handler { return &handler{b: b, inner: inner} }

type handler struct {
	b     *Buffer
	inner slog.Handler
	attrs []slog.Attr
	group string
}

func (h *handler) Enabled(ctx context.Context, l slog.Level) bool { return h.inner.Enabled(ctx, l) }

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	var sb strings.Builder
	write := func(a slog.Attr) {
		if sb.Len() > 0 {
			sb.WriteByte(' ')
		}
		key := a.Key
		if h.group != "" {
			key = h.group + "." + key
		}
		fmt.Fprintf(&sb, "%s=%v", key, a.Value.Resolve())
	}
	for _, a := range h.attrs {
		write(a)
	}
	r.Attrs(func(a slog.Attr) bool { write(a); return true })
	h.b.add(Entry{Time: r.Time, Level: strings.ToLower(r.Level.String()), Message: r.Message, Source: h.b.Source, Attrs: sb.String()})
	return h.inner.Handle(ctx, r)
}

func (h *handler) WithAttrs(as []slog.Attr) slog.Handler {
	return &handler{b: h.b, inner: h.inner.WithAttrs(as), attrs: append(append([]slog.Attr{}, h.attrs...), as...), group: h.group}
}

func (h *handler) WithGroup(name string) slog.Handler {
	g := name
	if h.group != "" {
		g = h.group + "." + name
	}
	return &handler{b: h.b, inner: h.inner.WithGroup(name), attrs: h.attrs, group: g}
}
