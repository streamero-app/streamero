// Package dlna is a UPnP AV / DLNA media server (MediaServer:1 with
// ContentDirectory:1 and ConnectionManager:1) so smart TVs can browse and play
// the library from their built-in media player (D-038). Written from the public
// UPnP Device Architecture, UPnP AV and DLNA guideline documents only.
package dlna

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/activity"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Server is the DLNA media server: SSDP on UDP 1900 and its own HTTP listener
// (description, control, media). The HTTP side has no request timeout, unlike
// the main router, because transcoded streams run for hours.
type Server struct {
	Pool       *pgxpool.Pool
	Auth       *auth.Service
	Library    *library.Service
	Stream     *stream.Server
	Settings   func() *settings.Store
	ServerID   uuid.UUID
	ServerName func(ctx context.Context) string
	Encoding   stream.Loader
	FFmpeg     string
	Version    string
	Log        *slog.Logger
	NowPlaying *activity.Registry
	// BaseURL overrides the announced address ("http://192.168.1.10:8788").
	BaseURL string
	// HTTPPort is the port of the HTTP listener (for announced URLs).
	HTTPPort int

	cache    atomic.Pointer[cached]
	tracker  tracker
	ssdp     *SSDP
	running  atomic.Int32 // transcodes in progress
	statusMu sync.Mutex
	status   Status
}

// Status is the listener state shown to administrators.
type Status struct {
	HTTPAddr   string
	HTTPUp     bool
	SSDPUp     bool
	Interfaces []string
	Error      string
}

type cached struct {
	at       time.Time
	settings Settings
	viewer   *Viewer // nil: inactive or the chosen user is gone
}

// Viewer is who DLNA browsing acts as.
type Viewer struct {
	Principal auth.Principal
	Owner     uuid.UUID
	Blocked   map[string]bool // official ratings above the profile's limit
	Prefs     library.Prefs
	Lang      string
}

// CanSeeLibraries mirrors the API rule: admins, or users allowed all libraries.
func (v *Viewer) CanSeeLibraries() bool {
	return v.Principal.User.IsAdmin || v.Principal.User.Permissions.AllLibraries
}

// UDN is the device id: stable per server, distinct from the server id.
func (s *Server) UDN() string {
	return "uuid:" + uuid.NewSHA1(s.ServerID, []byte("streamero-dlna")).String()
}

// Reload drops the cached settings (after an administrator changed them).
func (s *Server) Reload() { s.cache.Store(nil) }

// Current returns the settings and the viewer (nil when DLNA is not active).
func (s *Server) Current(ctx context.Context) (Settings, *Viewer) {
	if c := s.cache.Load(); c != nil && time.Since(c.at) < 10*time.Second {
		return c.settings, c.viewer
	}
	var st Settings
	if s.Settings != nil && s.Settings() != nil {
		_, _ = s.Settings().Get(ctx, SettingsKey, &st)
	}
	st = st.Normalize()
	c := &cached{at: time.Now(), settings: st}
	if st.Active() {
		if v, err := s.viewer(ctx, st); err == nil {
			c.viewer = v
		} else if s.Log != nil {
			s.Log.Warn("dlna: the configured user cannot be used", "err", err)
		}
	}
	s.cache.Store(c)
	return st, c.viewer
}

func (s *Server) viewer(ctx context.Context, st Settings) (*Viewer, error) {
	p, err := s.Auth.ActAs(ctx, *st.UserID, st.ProfileID)
	if err != nil {
		return nil, err
	}
	v := &Viewer{Principal: p, Owner: stream.Owner(p), Blocked: map[string]bool{}, Lang: st.Language}
	v.Prefs, _ = s.Library.GetPrefs(ctx, v.Owner)
	if p.Profile != nil && p.Profile.MaxRating != nil {
		rows, err := s.Pool.Query(ctx, `SELECT DISTINCT official_rating FROM items WHERE official_rating IS NOT NULL`)
		if err != nil {
			return nil, err
		}
		var known []string
		for rows.Next() {
			var r string
			if rows.Scan(&r) == nil {
				known = append(known, r)
			}
		}
		rows.Close()
		for _, r := range library.BlockedRatings(known, *p.Profile.MaxRating) {
			v.Blocked[r] = true
		}
	}
	return v, nil
}

// active reports whether DLNA serves at all right now.
func (s *Server) active(ctx context.Context) bool {
	_, v := s.Current(ctx)
	return v != nil
}

// FriendlyName is the name TVs show.
func (s *Server) FriendlyName(ctx context.Context) string {
	st, _ := s.Current(ctx)
	if st.FriendlyName != "" {
		return st.FriendlyName
	}
	if s.ServerName != nil {
		return s.ServerName(ctx)
	}
	return "Streamero"
}

// GetStatus returns the listener state.
func (s *Server) GetStatus() Status {
	s.statusMu.Lock()
	defer s.statusMu.Unlock()
	st := s.status
	if s.ssdp != nil {
		st.Interfaces = s.ssdp.Interfaces()
	}
	return st
}

func (s *Server) setStatus(f func(*Status)) {
	s.statusMu.Lock()
	f(&s.status)
	s.statusMu.Unlock()
}

// serverHeader is the SERVER / User-Agent value ("OS/version UPnP/1.0 product/version").
func (s *Server) serverHeader() string {
	return "Linux/1.0 UPnP/1.0 DLNADOC/1.50 Streamero/" + s.Version
}

// baseFor returns the URL prefix TVs reach this server at, for a request.
var hostRE = regexp.MustCompile(`^[A-Za-z0-9.\-]+(:[0-9]{1,5})?$|^\[[0-9A-Fa-f:.]+\](:[0-9]{1,5})?$`)

func (s *Server) baseFor(r *http.Request) string {
	if s.BaseURL != "" {
		return s.BaseURL
	}
	if hostRE.MatchString(r.Host) {
		return "http://" + r.Host
	}
	if la, ok := r.Context().Value(http.LocalAddrContextKey).(net.Addr); ok {
		return "http://" + la.String()
	}
	return "http://127.0.0.1:" + strconv.Itoa(s.port())
}

func (s *Server) port() int {
	if s.HTTPPort == 0 {
		return 8788
	}
	return s.HTTPPort
}

// location is the description URL announced on a local address.
func (s *Server) location(local netip.Addr) string {
	if s.BaseURL != "" {
		return s.BaseURL + PathDescription
	}
	return "http://" + netip.AddrPortFrom(local.WithZone(""), uint16(s.port())).String() + PathDescription // #nosec G115 -- port from config
}

// Handler returns the HTTP side.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET "+PathDescription, s.description)
	mux.HandleFunc("GET "+pathCDSSCPD, xmlDoc(CDSDescription))
	mux.HandleFunc("GET "+pathCMSSCPD, xmlDoc(CMSDescription))
	mux.HandleFunc("POST "+pathCDSControl, s.controlCDS)
	mux.HandleFunc("POST "+pathCMSControl, s.controlCMS)
	mux.HandleFunc(pathCDSEvents, s.events)
	mux.HandleFunc(pathCMSEvents, s.events)
	mux.HandleFunc("GET /dlna/f/{fileId}/{name}", s.direct) // GET covers HEAD
	mux.HandleFunc("GET /dlna/t/{fileId}/{name}", s.transcode)
	mux.HandleFunc("GET /dlna/s/{fileId}/{name}", s.subtitle)
	mux.HandleFunc("GET /dlna/a/{itemId}/{name}", s.art)
	return s.guard(mux)
}

// guard answers only local-network clients, and nothing while DLNA is off.
func (s *Server) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ap, err := netip.ParseAddrPort(r.RemoteAddr)
		if err != nil || !isLocal(ap.Addr()) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		if !s.active(r.Context()) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Server", s.serverHeader())
		next.ServeHTTP(w, r)
	})
}

func xmlDoc(body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", `text/xml; charset="utf-8"`)
		_, _ = w.Write([]byte(body))
	}
}

func (s *Server) description(w http.ResponseWriter, r *http.Request) {
	xmlDoc(DeviceDescription(DeviceInfo{UDN: s.UDN(), FriendlyName: s.FriendlyName(r.Context()),
		Version: s.Version, Serial: s.ServerID.String()}))(w, r)
}

// events accepts GENA subscriptions. State changes are not pushed (the tree
// changes on every scan and TVs re-browse); the initial event the spec asks
// for is sent so renderers that wait for it carry on.
func (s *Server) events(w http.ResponseWriter, r *http.Request) {
	switch r.Method {
	case "SUBSCRIBE":
		sid := r.Header.Get("SID")
		if sid == "" {
			sid = "uuid:" + uuid.NewString()
			if cb := callbackURL(r.Header.Get("CALLBACK")); cb != "" && sameHost(cb, r.RemoteAddr) {
				go s.initialEvent(cb, sid, r.URL.Path)
			}
		}
		setExact(w.Header(), "SID", sid)
		setExact(w.Header(), "TIMEOUT", "Second-1800")
		w.WriteHeader(http.StatusOK)
	case "UNSUBSCRIBE":
		w.WriteHeader(http.StatusOK)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
}

// callbackURL takes the first "<http://…>" of a CALLBACK header.
func callbackURL(h string) string {
	start, end := -1, -1
	for i, c := range h {
		if c == '<' && start < 0 {
			start = i + 1
		}
		if c == '>' && start >= 0 {
			end = i
			break
		}
	}
	if start < 0 || end <= start {
		return ""
	}
	return h[start:end]
}

// sameHost: event callbacks only go back to the subscriber itself (no requests
// to third hosts on its behalf).
func sameHost(cb, remote string) bool {
	u, err := url.Parse(cb)
	if err != nil || u.Scheme != "http" {
		return false
	}
	ap, err := netip.ParseAddrPort(remote)
	if err != nil {
		return false
	}
	ip, err := netip.ParseAddr(u.Hostname())
	return err == nil && ip.Unmap() == ap.Addr().Unmap()
}

func (s *Server) initialEvent(cb, sid, path string) {
	body := `<?xml version="1.0"?><e:propertyset xmlns:e="urn:schemas-upnp-org:event-1-0"><e:property>`
	if path == pathCDSEvents {
		body += `<SystemUpdateID>` + strconv.FormatUint(uint64(s.systemUpdateID(context.Background())), 10) + `</SystemUpdateID>`
	} else {
		body += `<SourceProtocolInfo>` + esc(sourceProtocols()) + `</SourceProtocolInfo><SinkProtocolInfo></SinkProtocolInfo><CurrentConnectionIDs>0</CurrentConnectionIDs>`
	}
	body += `</e:property></e:propertyset>`
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// #nosec G704 -- cb is the subscriber's own LAN address (checked by sameHost)
	req, err := http.NewRequestWithContext(ctx, "NOTIFY", cb, strings.NewReader(body))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", `text/xml; charset="utf-8"`)
	req.Header.Set("NT", "upnp:event")
	req.Header.Set("NTS", "upnp:propchange")
	req.Header.Set("SID", sid)
	req.Header.Set("SEQ", "0")
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req) // #nosec G704 -- callback is the subscriber's own LAN address (sameHost)
	if err == nil {
		_ = resp.Body.Close()
	}
}

// Run starts the HTTP listener and SSDP until ctx ends. Listener errors are
// recorded in the status and logged, never fatal: DLNA is optional.
func (s *Server) Run(ctx context.Context, httpAddr, ssdpListen string) {
	s.setStatus(func(st *Status) { st.HTTPAddr = httpAddr })
	go s.tracker.reap(ctx, s)
	ln, err := (&net.ListenConfig{}).Listen(ctx, "tcp", httpAddr)
	if err != nil {
		s.Log.Warn("dlna http listener unavailable", "addr", httpAddr, "err", err)
		s.setStatus(func(st *Status) { st.Error = err.Error() })
		return
	}
	srv := &http.Server{Handler: s.Handler(), ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() { // #nosec G118 -- shutdown runs after ctx ended and needs a fresh deadline
		<-ctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(sctx)
	}()
	go func() {
		s.setStatus(func(st *Status) { st.HTTPUp = true })
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			s.Log.Warn("dlna http listener stopped", "err", err)
		}
		s.setStatus(func(st *Status) { st.HTTPUp = false })
	}()
	s.ssdp = &SSDP{UDN: s.UDN(), Location: s.location, Server: s.serverHeader(), Log: s.Log,
		Active: func() bool { return s.active(context.Background()) }}
	go func() {
		s.setStatus(func(st *Status) { st.SSDPUp = true })
		if err := s.ssdp.ListenAndServe(ctx, ssdpListen); err != nil {
			s.Log.Warn("dlna discovery (SSDP) unavailable", "addr", ssdpListen, "err", err)
			s.setStatus(func(st *Status) { st.Error = err.Error() })
		}
		s.setStatus(func(st *Status) { st.SSDPUp = false })
	}()
	s.Log.Info("dlna listening", "http", httpAddr, "ssdp", ssdpListen)
}
