package livetv

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

// maxImageBytes bounds a cached logo or programme image.
const maxImageBytes = 5 << 20

// ImageURL signs a link to a remote logo or programme image served through
// /stream/img (players and image views cannot send credentials, and an http
// image would be blocked on an https page). The remote URL is in the path, so
// the signature binds it: only images the guide or playlist named are fetched.
// Links stay the same for a day (browser caching).
func (s *Service) ImageURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || len(raw) > 2000 {
		return ""
	}
	if _, err := httpURL(raw); err != nil {
		return ""
	}
	key := base64.RawURLEncoding.EncodeToString([]byte(raw))
	scope := "/stream/img/" + key + "/"
	exp := s.now().UTC().Truncate(24 * time.Hour).Add(48 * time.Hour)
	return scope + "image?" + s.Signer.QueryUntil(scope, exp)
}

// ImageHandler serves /stream/img/{key}/{name}; the stream router has checked the signature.
func (s *Service) ImageHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, err := base64.RawURLEncoding.DecodeString(chi.URLParam(r, "key"))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		raw := string(b)
		if _, err := httpURL(raw); err != nil {
			http.NotFound(w, r)
			return
		}
		sum := sha256.Sum256(b)
		name := hex.EncodeToString(sum[:])
		dir := s.ImageDir
		if dir == "" {
			dir = filepath.Join(os.TempDir(), "streamero-livetv-images")
		}
		path := filepath.Join(dir, name)
		if data, err := os.ReadFile(path); err == nil { //nolint:gosec // G304/G703: hex hash name inside our cache folder
			serveImage(w, data)
			return
		}
		//nolint:gosec // G704: the URL is signed by us (named by a guide or playlist); the client refuses blocked addresses
		req, err := http.NewRequestWithContext(r.Context(), http.MethodGet, raw, nil)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		req.Header.Set("User-Agent", "Streamero")
		resp, err := s.http().Do(req) // #nosec G704 -- URL signed by us (from a guide or playlist); blocked addresses refused
		if err != nil {
			http.Error(w, "image unavailable", http.StatusBadGateway)
			return
		}
		defer func() { _ = resp.Body.Close() }()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, "image unavailable", http.StatusBadGateway)
			return
		}
		data, err := readLimited(resp.Body, maxImageBytes)
		if err != nil {
			http.Error(w, "image unavailable", http.StatusBadGateway)
			return
		}
		if !strings.HasPrefix(http.DetectContentType(data), "image/") && !isSVG(data) {
			http.Error(w, "not an image", http.StatusBadGateway)
			return
		}
		if os.MkdirAll(dir, 0o750) == nil {
			tmp, err := os.CreateTemp(dir, ".img-*")
			if err == nil {
				_, werr := tmp.Write(data)
				_ = tmp.Close()
				if werr == nil {
					_ = os.Rename(tmp.Name(), path) //nolint:gosec // G703: hex hash name inside our cache folder
				} else {
					_ = os.Remove(tmp.Name())
				}
			}
		}
		serveImage(w, data)
	})
}

func isSVG(data []byte) bool {
	head := strings.ToLower(string(data[:min(len(data), 512)]))
	return strings.Contains(head, "<svg")
}

func serveImage(w http.ResponseWriter, data []byte) {
	ct := http.DetectContentType(data)
	if isSVG(data) && !strings.HasPrefix(ct, "image/") {
		ct = "image/svg+xml"
		// An SVG may carry script: never run it in our origin.
		w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	}
	w.Header().Set("Content-Type", ct)
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	_, _ = io.Copy(w, strings.NewReader(string(data)))
}
