package arr

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/streamero-app/streamero/server/internal/requests"
)

const (
	statusTimeout = 4 * time.Second
	statusTTL     = 30 * time.Second
)

// Status is one service as GET /arr/services reports it.
type Status struct {
	ID         string
	Configured bool
	Reachable  bool
	Version    string
	Error      string
	APIBase    string
}

type statusEntry struct {
	at  time.Time
	key [32]byte // what was checked: base URL and key
	st  Status
}

type statusCache struct {
	mu      sync.Mutex
	entries map[string]statusEntry
}

// Statuses checks every service (in parallel, cached for 30 s per base URL and key).
func (p *Proxy) Statuses(ctx context.Context, st requests.Settings) []Status {
	p.init()
	out := make([]Status, len(Services))
	var wg sync.WaitGroup
	for i, s := range Services {
		base, key := s.Conn(st)
		out[i] = Status{ID: s.ID, APIBase: s.APIBase, Configured: base != "" && key != ""}
		if !out[i].Configured {
			continue
		}
		fp := sha256.Sum256([]byte(base + "\x00" + key))
		if e, ok := p.status.get(s.ID, fp); ok {
			out[i] = e
			continue
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			v, err := p.version(ctx, s, base, key)
			res := out[i]
			if err != nil {
				res.Error = err.Error()
			} else {
				res.Reachable, res.Version = true, v
			}
			out[i] = res
			p.status.put(s.ID, fp, res)
		}()
	}
	wg.Wait()
	return out
}

func (c *statusCache) get(id string, fp [32]byte) (Status, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	e, ok := c.entries[id]
	if !ok || e.key != fp || time.Since(e.at) > statusTTL {
		return Status{}, false
	}
	return e.st, true
}

func (c *statusCache) put(id string, fp [32]byte, st Status) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = map[string]statusEntry{}
	}
	c.entries[id] = statusEntry{at: time.Now(), key: fp, st: st}
}

// version asks the service's status endpoint for its version.
func (p *Proxy) version(ctx context.Context, s Service, base, key string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	target, err := Target(base, s.StatusPath, "")
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", err
	}
	req.Header = OutboundHeaders(http.Header{"Accept": {"application/json"}}, s, key)
	resp, err := p.HTTP.Do(req) //nolint:gosec // G704: the base URL is set by an administrator
	if err != nil {
		return "", fmt.Errorf("%s is not reachable", s.Name)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		return "", fmt.Errorf("%s refused the API key (HTTP %d)", s.Name, resp.StatusCode)
	}
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("%s answered HTTP %d", s.Name, resp.StatusCode)
	}
	var body struct {
		Version string `json:"version"`
		Data    struct {
			Version string `json:"bazarr_version"`
		} `json:"data"`
	}
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&body); err != nil {
		return "", fmt.Errorf("%s did not answer like %s", base, s.Name)
	}
	if body.Version == "" {
		body.Version = body.Data.Version
	}
	return body.Version, nil
}
