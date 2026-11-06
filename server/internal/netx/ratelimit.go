package netx

import (
	"encoding/json"
	"net/http"
	"net/netip"
	"strconv"
	"sync"
	"time"
)

// Limiter is a per-client token bucket kept in memory (one server process).
type Limiter struct {
	mu      sync.Mutex
	rate    float64 // tokens per second
	burst   float64
	clients map[netip.Addr]*bucket
	now     func() time.Time
}

type bucket struct {
	tokens float64
	last   time.Time
}

// NewLimiter allows burst requests at once and perMinute on average per client.
func NewLimiter(perMinute, burst int) *Limiter {
	return &Limiter{rate: float64(perMinute) / 60, burst: float64(burst), clients: map[netip.Addr]*bucket{}, now: time.Now}
}

// Allow takes a token for ip; when empty it returns false and the wait until the next token.
func (l *Limiter) Allow(ip netip.Addr) (bool, time.Duration) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	b, ok := l.clients[ip]
	if !ok {
		if len(l.clients) > 100_000 { // bound memory under address spraying
			l.evict(now)
		}
		b = &bucket{tokens: l.burst, last: now}
		l.clients[ip] = b
	}
	b.tokens = min(l.burst, b.tokens+now.Sub(b.last).Seconds()*l.rate)
	b.last = now
	if b.tokens >= 1 {
		b.tokens--
		return true, 0
	}
	return false, time.Duration((1 - b.tokens) / l.rate * float64(time.Second))
}

// evict drops buckets that have refilled completely (they carry no state).
func (l *Limiter) evict(now time.Time) {
	full := time.Duration(l.burst / l.rate * float64(time.Second))
	for ip, b := range l.clients {
		if now.Sub(b.last) > full {
			delete(l.clients, ip)
		}
	}
}

// Limit answers 429 with Retry-After when a client exceeds the limiter on
// requests for which match returns true.
func Limit(l *Limiter, match func(*http.Request) bool) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if match(r) {
				if ok, wait := l.Allow(FromContext(r.Context())); !ok {
					secs := int(wait.Seconds()) + 1
					w.Header().Set("Retry-After", strconv.Itoa(secs))
					w.Header().Set("Content-Type", "application/json")
					w.WriteHeader(http.StatusTooManyRequests)
					_ = json.NewEncoder(w).Encode(map[string]string{"code": "rate_limited", "message": "too many requests; slow down"})
					return
				}
			}
			next.ServeHTTP(w, r)
		})
	}
}
