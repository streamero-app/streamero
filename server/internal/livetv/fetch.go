package livetv

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/streamero-app/streamero/server/internal/library"
)

// ErrBlockedAddress means a URL points at an address the server never fetches
// from (loopback, link-local such as cloud metadata, unspecified).
var ErrBlockedAddress = errors.New("this address is not allowed")

// ErrBadSource means a source has neither a usable URL nor a usable file.
var ErrBadSource = errors.New("give an http(s) URL or a file inside the allowed folders")

// blockedIP reports addresses that are never fetched. Private LAN addresses
// are allowed: tuners and guide grabbers usually live there.
func blockedIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsInterfaceLocalMulticast()
}

// NewHTTPClient returns a client for guide, playlist, tuner and image fetches
// that refuses blocked addresses (checked after DNS resolution, on every
// connection including redirects). allowLoopback is for tests.
func NewHTTPClient(timeout time.Duration, allowLoopback bool) *http.Client {
	d := &net.Dialer{Timeout: 10 * time.Second, Control: func(_, address string, _ syscall.RawConn) error {
		host, _, err := net.SplitHostPort(address)
		if err != nil {
			return err
		}
		ip := net.ParseIP(host)
		if ip == nil {
			return ErrBlockedAddress
		}
		if allowLoopback && ip.IsLoopback() {
			return nil
		}
		if blockedIP(ip) {
			return ErrBlockedAddress
		}
		return nil
	}}
	tr := &http.Transport{DialContext: d.DialContext, Proxy: nil, MaxIdleConns: 10, IdleConnTimeout: 30 * time.Second,
		TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 30 * time.Second}
	return &http.Client{Transport: tr, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
			return errors.New("redirect to a non-HTTP address")
		}
		return nil
	}}
}

func httpURL(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, ErrBadSource
	}
	return u, nil
}

// Source is where a playlist or guide comes from: an http(s) URL or a file.
type Source struct {
	URL       string
	FilePath  string
	UserAgent string
	// Conditional request values from the last fetch (URL only).
	ETag, LastModified string
}

// Fetched is an opened source.
type Fetched struct {
	Body         io.ReadCloser
	NotModified  bool // the server answered 304
	ETag         string
	LastModified string
	ModTime      time.Time // files only
}

// open reads a source. Files must resolve inside roots (library.ResolveSafePath).
func open(ctx context.Context, c *http.Client, roots library.Roots, s Source) (Fetched, error) {
	if strings.TrimSpace(s.FilePath) != "" {
		p, err := library.ResolveSafePath(roots, strings.TrimSpace(s.FilePath))
		if err != nil {
			return Fetched{}, ErrBadSource
		}
		f, err := os.Open(p) // #nosec G304 -- validated by ResolveSafePath
		if err != nil {
			return Fetched{}, fmt.Errorf("open %s: %w", s.FilePath, err)
		}
		st, err := f.Stat()
		if err != nil || st.IsDir() {
			_ = f.Close()
			return Fetched{}, ErrBadSource
		}
		return Fetched{Body: f, ModTime: st.ModTime()}, nil
	}
	u, err := httpURL(s.URL)
	if err != nil {
		return Fetched{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return Fetched{}, err
	}
	req.Header.Set("User-Agent", userAgentOr(s.UserAgent))
	if s.ETag != "" {
		req.Header.Set("If-None-Match", s.ETag)
	}
	if s.LastModified != "" {
		req.Header.Set("If-Modified-Since", s.LastModified)
	}
	resp, err := c.Do(req) // #nosec G704 -- admin-configured source; blocked addresses refused by the dialer
	if err != nil {
		return Fetched{}, err
	}
	if resp.StatusCode == http.StatusNotModified {
		_ = resp.Body.Close()
		return Fetched{NotModified: true, ETag: s.ETag, LastModified: s.LastModified}, nil
	}
	if resp.StatusCode != http.StatusOK {
		_ = resp.Body.Close()
		return Fetched{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return Fetched{Body: resp.Body, ETag: resp.Header.Get("ETag"), LastModified: resp.Header.Get("Last-Modified")}, nil
}

func userAgentOr(ua string) string {
	if strings.TrimSpace(ua) != "" {
		return strings.TrimSpace(ua)
	}
	return "Streamero"
}
