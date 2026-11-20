package stream

import (
	"container/list"
	"context"
	"errors"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"
)

// Subtitle conversion runs ffmpeg over the whole file, so anyone holding a link
// could otherwise start any number of them by repeating the request (D-053).
// Conversions are cached (by source file, track, format and the source's size
// and modification time) and at most subtitleWorkers run at once.
const (
	subtitleWorkers    = 4
	subtitleCacheBytes = 64 << 20
	subtitleMaxBytes   = 16 << 20 // a larger result is served once, not cached
	subtitleWait       = 30 * time.Second
)

// errSubtitleBusy is returned when no conversion slot frees up in time.
var errSubtitleBusy = errors.New("too many subtitle conversions at once")

type subtitleCache struct {
	once  sync.Once
	slots chan struct{}

	mu    sync.Mutex
	order *list.List // of *subEntry, most recent first
	byKey map[string]*list.Element
	bytes int

	flightMu sync.Mutex
	flight   map[string]*subFlight
}

type subEntry struct {
	key  string
	data []byte
}

type subFlight struct {
	done chan struct{}
	data []byte
	err  error
}

func (c *subtitleCache) init() {
	c.once.Do(func() {
		c.slots = make(chan struct{}, subtitleWorkers)
		c.order = list.New()
		c.byKey = map[string]*list.Element{}
		c.flight = map[string]*subFlight{}
	})
}

// subtitleKey identifies a conversion; a changed source file gives a new key.
func subtitleKey(fileID uuid.UUID, index int, muxer, source string) string {
	k := fileID.String() + "/" + strconv.Itoa(index) + "/" + muxer
	if fi, err := os.Stat(source); err == nil {
		k += "/" + strconv.FormatInt(fi.Size(), 10) + "/" + strconv.FormatInt(fi.ModTime().UnixNano(), 10)
	}
	return k
}

func (c *subtitleCache) get(key string) ([]byte, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if el, ok := c.byKey[key]; ok {
		c.order.MoveToFront(el)
		return el.Value.(*subEntry).data, true
	}
	return nil, false
}

func (c *subtitleCache) put(key string, data []byte) {
	if len(data) > subtitleMaxBytes {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.byKey[key]; ok {
		return
	}
	c.byKey[key] = c.order.PushFront(&subEntry{key: key, data: data})
	c.bytes += len(data)
	for c.bytes > subtitleCacheBytes {
		el := c.order.Back()
		e := el.Value.(*subEntry)
		c.order.Remove(el)
		delete(c.byKey, e.key)
		c.bytes -= len(e.data)
	}
}

// convert returns the cached result for key, or runs fn once for all callers
// asking at the same time, within the worker limit.
func (c *subtitleCache) convert(ctx context.Context, key string, fn func(context.Context) ([]byte, error)) ([]byte, error) {
	c.init()
	if data, ok := c.get(key); ok {
		return data, nil
	}
	c.flightMu.Lock()
	if f, ok := c.flight[key]; ok {
		c.flightMu.Unlock()
		select {
		case <-f.done:
			return f.data, f.err
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	f := &subFlight{done: make(chan struct{})}
	c.flight[key] = f
	c.flightMu.Unlock()
	defer func() {
		c.flightMu.Lock()
		delete(c.flight, key)
		c.flightMu.Unlock()
		close(f.done)
	}()

	wait, cancel := context.WithTimeout(ctx, subtitleWait)
	defer cancel()
	select {
	case c.slots <- struct{}{}:
	case <-wait.Done():
		f.err = errSubtitleBusy
		return nil, f.err
	}
	defer func() { <-c.slots }()
	f.data, f.err = fn(ctx)
	if f.err == nil {
		c.put(key, f.data)
	}
	return f.data, f.err
}
