package arr

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/streamero-app/streamero/server/internal/netx"
)

const (
	// MaxBody caps proxied request bodies (settings backups, large JSON lists).
	MaxBody = 16 << 20
	// HeaderTimeout bounds the wait for the service's response headers; the
	// body (logs, backups) may stream for as long as the client stays.
	HeaderTimeout = 60 * time.Second
	// imageCacheControl lets clients keep artwork for a day.
	imageCacheControl = "private, max-age=86400"
	// sandboxCSP keeps any HTML or SVG a service returns inert in a browser.
	sandboxCSP = "default-src 'none'; sandbox"
)

// Proxy forwards administrator requests to the *arr services.
type Proxy struct {
	// HTTP is the upstream client (nil = default: no overall timeout, same-host
	// redirects only).
	HTTP *http.Client
	// Rate and Burst limit requests per user (0 = 20/s, burst 60).
	Rate, Burst float64

	once    sync.Once
	limiter *limiter
	status  statusCache
}

func (p *Proxy) init() {
	p.once.Do(func() {
		rate, burst := p.Rate, p.Burst
		if rate <= 0 {
			rate = 20
		}
		if burst <= 0 {
			burst = 60
		}
		p.limiter = &limiter{rate: rate, burst: burst, users: map[uuid.UUID]*bucket{}, now: time.Now}
		if p.HTTP == nil {
			p.HTTP = &http.Client{CheckRedirect: netx.SameHostRedirects}
		}
	})
}

// WriteError writes the API's usual error body.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code, "message": message})
}

// OutboundHeaders builds the headers sent to the service: only Content-Type,
// Accept and Accept-Language from the client (never its Authorization, cookies,
// keys or hop-by-hop headers), plus the stored key in the service's header.
func OutboundHeaders(in http.Header, s Service, key string) http.Header {
	out := http.Header{}
	for _, h := range []string{"Content-Type", "Accept", "Accept-Language"} {
		if v := in.Values(h); len(v) > 0 {
			out[h] = append([]string(nil), v...)
		}
	}
	// Set as written (not canonicalised): Bazarr documents X-API-KEY.
	out[s.KeyHeader] = []string{key}
	return out
}

// passHeaders are the response headers passed back to the client; Set-Cookie,
// hop-by-hop and everything else stay behind.
var passHeaders = []string{"Content-Type", "Content-Length", "Content-Disposition", "Cache-Control", "ETag", "Last-Modified"}

// CopyResponseHeaders copies the allowed response headers. Images get a client
// cache lifetime; everything gets an inert content policy.
func CopyResponseHeaders(dst, src http.Header, image bool) {
	for _, h := range passHeaders {
		if v := src.Values(h); len(v) > 0 {
			dst[http.CanonicalHeaderKey(h)] = append([]string(nil), v...)
		}
	}
	if image {
		dst.Set("Cache-Control", imageCacheControl)
	}
	dst.Set("Content-Security-Policy", sandboxCSP)
}

// Serve forwards one request. The caller has authenticated an administrator
// (user) and resolved the service; escaped is the raw sub-path after /proxy/.
func (p *Proxy) Serve(w http.ResponseWriter, r *http.Request, user uuid.UUID, s Service, base, key, escaped string) {
	p.init()
	start := time.Now()
	status := 0
	defer func() {
		// Never the key or the query string: only what was called and how it went.
		slog.DebugContext(r.Context(), "arr proxy", "service", s.ID, "method", r.Method, "path", escaped,
			"status", status, "duration", time.Since(start))
	}()
	fail := func(st int, code, msg string) {
		status = st
		WriteError(w, st, code, msg)
	}
	if !s.AllowsMethod(r.Method) {
		w.Header().Set("Allow", allowed(s))
		fail(http.StatusMethodNotAllowed, "method_not_allowed", "method not allowed")
		return
	}
	if ok, wait := p.limiter.allow(user); !ok {
		w.Header().Set("Retry-After", strconv.Itoa(int(wait.Seconds())+1))
		fail(http.StatusTooManyRequests, "rate_limited", "too many requests; slow down")
		return
	}
	path, err := CleanPath(s, escaped)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_path", "this path is not allowed")
		return
	}
	if s.isFilePath(path) && r.Method != http.MethodGet {
		w.Header().Set("Allow", http.MethodGet)
		fail(http.StatusMethodNotAllowed, "method_not_allowed", "files can only be downloaded")
		return
	}
	if base == "" || key == "" {
		fail(http.StatusConflict, "arr_not_configured", s.Name+" is not set up")
		return
	}
	if r.ContentLength > MaxBody {
		fail(http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		return
	}
	target, err := Target(base, path, FilterQuery(r.URL.RawQuery))
	if err != nil {
		fail(http.StatusConflict, "arr_not_configured", s.Name+" address is not valid")
		return
	}

	// The upstream request lives as long as the client connection; only the
	// wait for response headers is bounded.
	ctx, cancel := context.WithCancel(r.Context())
	defer cancel()
	var body io.Reader
	var lb *limitedBody
	if r.Body != nil && r.Body != http.NoBody && r.Method != http.MethodGet {
		lb = &limitedBody{r: r.Body, left: MaxBody}
		body = lb
	}
	out, err := http.NewRequestWithContext(ctx, r.Method, target.String(), body)
	if err != nil {
		fail(http.StatusBadRequest, "invalid_path", "this path is not allowed")
		return
	}
	if body != nil {
		out.ContentLength = r.ContentLength
	}
	out.Header = OutboundHeaders(r.Header, s, key)
	timer := time.AfterFunc(HeaderTimeout, cancel)
	resp, err := p.HTTP.Do(out) //nolint:gosec // G704: the base URL is set by an administrator
	timer.Stop()
	if err != nil {
		switch {
		case lb != nil && lb.over:
			fail(http.StatusRequestEntityTooLarge, "request_too_large", "request body too large")
		case errors.Is(err, netx.ErrCrossHostRedirect):
			fail(http.StatusBadGateway, "arr_unreachable", s.Name+" redirected to another host; refused")
		case r.Context().Err() != nil:
			status = 499 // client went away
		default:
			fail(http.StatusBadGateway, "arr_unreachable", s.Name+" is not reachable")
		}
		return
	}
	defer func() { _ = resp.Body.Close() }()
	image := r.Method == http.MethodGet && resp.StatusCode == http.StatusOK &&
		(s.isImagePath(path) || strings.HasPrefix(resp.Header.Get("Content-Type"), "image/"))
	CopyResponseHeaders(w.Header(), resp.Header, image)
	status = resp.StatusCode
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(flushWriter{w}, resp.Body)
}

func allowed(s Service) string {
	if s.Patch {
		return "GET, POST, PUT, PATCH, DELETE"
	}
	return "GET, POST, PUT, DELETE"
}

// limitedBody stops reading past MaxBody (bodies without a Content-Length).
type limitedBody struct {
	r    io.Reader
	left int64
	over bool
}

func (l *limitedBody) Read(b []byte) (int, error) {
	if l.left <= 0 {
		// One byte more than allowed tells "exactly at the limit" from "over".
		var one [1]byte
		if n, _ := l.r.Read(one[:]); n > 0 {
			l.over = true
			return 0, errors.New("request body too large")
		}
		return 0, io.EOF
	}
	if int64(len(b)) > l.left {
		b = b[:l.left]
	}
	n, err := l.r.Read(b)
	l.left -= int64(n)
	return n, err
}

// flushWriter sends each chunk on (log tails, long downloads) instead of
// holding it in the server's buffer.
type flushWriter struct{ w http.ResponseWriter }

func (f flushWriter) Write(b []byte) (int, error) {
	n, err := f.w.Write(b)
	if err == nil {
		_ = http.NewResponseController(f.w).Flush()
	}
	return n, err
}

// limiter is a per-user token bucket.
type limiter struct {
	mu          sync.Mutex
	rate, burst float64
	users       map[uuid.UUID]*bucket
	now         func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

func (l *limiter) allow(user uuid.UUID) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.users[user]
	if !ok {
		if len(l.users) > 10_000 {
			for id, old := range l.users {
				if now.Sub(old.last) > time.Minute {
					delete(l.users, id)
				}
			}
		}
		b = &bucket{tokens: l.burst, last: now}
		l.users[user] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}
