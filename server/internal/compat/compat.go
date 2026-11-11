// Package compat serves the subset of the Jellyfin-style HTTP API that request
// managers (Seerr/Jellyseerr), the *arr "Emby/Jellyfin" connection and media
// clients (Infuse, Swiftfin, the Kodi add-on, TV apps) use. It is written from
// the public OpenAPI description and observed client behaviour only (clean-room,
// D-011/D-016/D-031). Only system info names the product the way those clients
// expect (D-031); everything else identifies the server as Streamero.
package compat

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/livetv"
	"github.com/streamero-app/streamero/server/internal/stream"
	"github.com/streamero-app/streamero/server/internal/syncplay"
)

// APIVersion is the API level this layer implements (what clients compare against).
const APIVersion = "10.11.0"

// ProductName is reported in system info. Clients of this API check it to
// recognise a compatible server (D-031); it is never shown in Streamero's own UI.
const ProductName = "Jellyfin Server"

// Server is the compat HTTP surface, mounted under a prefix (default /compat).
type Server struct {
	Pool       *pgxpool.Pool
	Auth       *auth.Service
	Libraries  *library.Service
	Stream     *stream.Server // nil = no playback (tests of the browse API)
	ServerID   uuid.UUID
	ServerName func(ctx context.Context) string
	Log        *slog.Logger
	// OnPlayback and OnProgress feed the admin dashboard's "now playing" (optional).
	OnPlayback func(ctx context.Context, p auth.Principal, itemID uuid.UUID, method, reason string, durationMS *int64, startMS int64)
	OnProgress func(p auth.Principal, itemID uuid.UUID, positionMS int64)
	// Commands delivers dashboard session commands over /socket (optional).
	Commands Commands
	// SyncPlay runs watch-together groups (optional, D-036); SyncIdentity
	// names a member (account or profile name, device).
	SyncPlay     *syncplay.Service
	SyncIdentity SyncIdentityFunc
	// LiveTV exposes channels and the guide (optional, D-041).
	LiveTV *livetv.Service

	routesOnce sync.Once
	routes     http.Handler

	keysMu sync.Mutex
	keys   []recentKey // secrets of keys created here, readable for a short time

	playsMu sync.Mutex
	plays   map[string]playMemo
}

type recentKey struct {
	app, secret string
	created     time.Time
}

// recentKeyTTL bounds how long a freshly created key can be read back. Clients
// create a key and immediately list keys to learn it; Streamero itself only
// stores hashes, so the plaintext lives in memory for this window only.
const recentKeyTTL = 10 * time.Minute

// Routes returns the handler (paths are case-insensitive like the reference API).
// It is built once per server.
func (s *Server) Routes() http.Handler {
	s.routesOnce.Do(func() { s.routes = s.handler(s.mux()) })
	return s.routes
}

// matcher is the route table without a server behind it, for Matches.
var matcher = sync.OnceValue(func() *chi.Mux { return (&Server{}).mux() })

// Matches reports whether the compat API has a route for the request (paths
// compared case-insensitively). The root router uses it to serve Jellyfin-style
// paths without the /compat prefix (D-032) and leave everything else to the web
// client. A CORS preflight matches when any method of the path does.
func Matches(method, path string) bool {
	p := strings.ToLower(path)
	if method == http.MethodOptions {
		for _, m := range []string{http.MethodGet, http.MethodPost, http.MethodDelete} {
			if matcher().Match(chi.NewRouteContext(), m, p) {
				return true
			}
		}
		return false
	}
	return matcher().Match(chi.NewRouteContext(), method, p)
}

// IsCompatPath reports whether a request path belongs to the compat API, under
// /compat or (when served at the root) at a Jellyfin-style path.
func IsCompatPath(method, path string, atRoot bool) bool {
	if len(path) >= len(Prefix) && strings.EqualFold(path[:len(Prefix)], Prefix) &&
		(len(path) == len(Prefix) || path[len(Prefix)] == '/') {
		return true
	}
	return atRoot && Matches(method, path)
}

// Prefix is where the compat API is always served.
const Prefix = "/compat"

func (s *Server) mux() *chi.Mux {
	r := chi.NewRouter()
	r.Use(lowercasePath)
	r.Get("/socket", s.socket)
	r.Get("/system/info/public", s.publicInfo)
	r.Get("/system/ping", s.ping)
	r.Post("/system/ping", s.ping)
	r.Get("/branding/configuration", branding)
	r.Get("/branding/css", brandingCSS)
	r.Get("/branding/css.css", brandingCSS)
	r.Get("/users/public", emptyList) // user names are not listed to anonymous callers
	r.Get("/quickconnect/enabled", quickConnectEnabled)
	r.Get("/quickconnect/initiate", s.quickConnectInitiate)
	r.Post("/quickconnect/initiate", s.quickConnectInitiate)
	r.Get("/quickconnect/connect", s.quickConnectConnect)
	r.Post("/users/authenticatewithquickconnect", s.quickConnectAuthenticate)
	r.Get("/web", webRedirect)
	r.Get("/web/*", webRedirect)
	r.Post("/users/authenticatebyname", s.authenticateByName)
	r.Get("/getutctime", utcTime)
	// Artwork is public like in the reference API (players load it without
	// headers); IDs are random UUIDs, person photos are public TMDb pictures.
	for _, p := range []string{"/items/{itemId}/images/{type}", "/items/{itemId}/images/{type}/{index}"} {
		r.Get(p, s.image)
		r.Head(p, s.image)
	}
	// Media bytes check the token or a signed media source tag themselves.
	for _, p := range []string{"/videos/{itemId}/{file}", "/audio/{itemId}/{file}"} {
		r.Get(p, s.media)
		r.Head(p, s.media)
	}
	r.Group(func(r chi.Router) {
		r.Use(s.requireAuth)
		r.Get("/system/info", s.info)
		r.Get("/system/configuration", s.configuration)
		r.Get("/system/endpoint", endpointInfo)
		r.Post("/notifications/admin", accepted)
		r.Get("/users/me", s.me)
		r.Get("/users", s.users)
		r.Get("/users/{userId}", s.user)
		r.Post("/quickconnect/authorize", s.quickConnectAuthorize)
		r.Get("/displaypreferences/{id}", displayPreferences)
		r.Post("/displaypreferences/{id}", accepted)
		r.Post("/sessions/capabilities", accepted)
		r.Post("/sessions/capabilities/full", accepted)
		r.Get("/sessions", emptyList)

		r.Get("/library/mediafolders", s.views)
		r.Get("/userviews", s.views)
		r.Get("/users/{userId}/views", s.views)
		r.Get("/items", s.items)
		r.Get("/users/{userId}/items", s.items)
		r.Get("/items/latest", s.latest)
		r.Get("/users/{userId}/items/latest", s.latest)
		r.Get("/useritems/resume", s.resume)
		r.Get("/users/{userId}/items/resume", s.resume)
		r.Get("/items/filters", s.filters(true))
		r.Get("/items/filters2", s.filters(false))
		r.Get("/genres", s.genres)
		r.Get("/search/hints", s.searchHints)
		r.Get("/items/{itemId}", s.item)
		r.Get("/users/{userId}/items/{itemId}", s.item)
		r.Get("/items/{itemId}/similar", s.similar)
		r.Get("/movies/{itemId}/similar", s.similar)
		r.Get("/shows/{itemId}/similar", s.similar)
		r.Get("/items/{itemId}/ancestors", s.ancestors)
		r.Get("/items/{itemId}/intros", emptyResult)
		r.Get("/users/{userId}/items/{itemId}/intros", emptyResult)
		r.Get("/items/{itemId}/localtrailers", emptyList)
		r.Get("/users/{userId}/items/{itemId}/localtrailers", emptyList)
		r.Get("/items/{itemId}/specialfeatures", emptyList)
		r.Get("/users/{userId}/items/{itemId}/specialfeatures", emptyList)
		r.Get("/items/{itemId}/thememedia", s.themeMedia)
		r.Get("/items/{itemId}/themesongs", s.themeList)
		r.Get("/items/{itemId}/themevideos", s.themeList)
		r.Get("/videos/{itemId}/additionalparts", emptyResult)
		r.Get("/shows/nextup", s.nextUp)
		r.Get("/shows/{seriesId}/seasons", s.seasons)
		r.Get("/shows/{seriesId}/episodes", s.episodes)

		r.Get("/items/{itemId}/playbackinfo", s.playbackInfo)
		r.Post("/items/{itemId}/playbackinfo", s.playbackInfo)
		r.Get("/videos/{itemId}/{sourceId}/subtitles/{index}/{file}", s.subtitle)
		r.Get("/videos/{itemId}/{sourceId}/subtitles/{index}/{ticks}/{file}", s.subtitle)
		r.Get("/mediasegments/{itemId}", s.segments)
		r.Get("/playback/bitratetest", bitrateTest)
		r.Post("/sessions/playing", s.playing("start"))
		r.Post("/sessions/playing/progress", s.playing("progress"))
		r.Post("/sessions/playing/stopped", s.playing("stop"))
		r.Post("/sessions/playing/ping", accepted)
		for _, prefix := range []string{"", "/users/{userId}"} {
			r.Post(prefix+"/playingitems/{itemId}", s.playing("start"))
			r.Post(prefix+"/playingitems/{itemId}/progress", s.playing("progress"))
			r.Delete(prefix+"/playingitems/{itemId}", s.playing("stop"))
		}

		r.Post("/userplayeditems/{itemId}", s.markUserData("played", true))
		r.Delete("/userplayeditems/{itemId}", s.markUserData("played", false))
		r.Post("/users/{userId}/playeditems/{itemId}", s.markUserData("played", true))
		r.Delete("/users/{userId}/playeditems/{itemId}", s.markUserData("played", false))
		r.Post("/userfavoriteitems/{itemId}", s.markUserData("favorite", true))
		r.Delete("/userfavoriteitems/{itemId}", s.markUserData("favorite", false))
		r.Post("/users/{userId}/favoriteitems/{itemId}", s.markUserData("favorite", true))
		r.Delete("/users/{userId}/favoriteitems/{itemId}", s.markUserData("favorite", false))
		r.Get("/useritems/{itemId}/userdata", s.getUserData)
		r.Post("/useritems/{itemId}/userdata", s.postUserData)
		r.Get("/users/{userId}/items/{itemId}/userdata", s.getUserData)
		r.Post("/users/{userId}/items/{itemId}/userdata", s.postUserData)

		s.syncRoutes(r)
		s.liveRoutes(r)

		r.Post("/library/refresh", s.refreshAll)
		r.Post("/library/media/updated", s.mediaUpdated)
		r.Post("/library/movies/updated", s.mediaUpdated)
		r.Post("/library/series/updated", s.mediaUpdated)
		r.Post("/sessions/logout", s.logout)
		r.Get("/auth/keys", s.listKeys)
		r.Post("/auth/keys", s.createKey)
	})
	return r
}

// corsAllowHeaders are the request headers clients of this API send. TV apps
// built as web apps (Samsung Tizen) call the server from their own origin.
const corsAllowHeaders = "Authorization, X-Emby-Authorization, X-Emby-Token, X-MediaBrowser-Token, " +
	"Content-Type, Accept, Accept-Language, Range, X-Requested-With, If-None-Match, If-Modified-Since"

// cors lets web-based clients call the API from another origin. Credentials
// are tokens in headers or the query, never cookies, so any origin is allowed
// and Access-Control-Allow-Credentials is never sent.
func cors(w http.ResponseWriter, r *http.Request) bool {
	if r.Header.Get("Origin") == "" {
		return false
	}
	h := w.Header()
	h.Set("Access-Control-Allow-Origin", "*")
	h.Set("Access-Control-Expose-Headers", "Content-Length, Content-Range, Accept-Ranges, ETag, Content-Disposition")
	if r.Method == http.MethodOptions && r.Header.Get("Access-Control-Request-Method") != "" {
		h.Set("Access-Control-Allow-Methods", "GET, HEAD, POST, DELETE, OPTIONS")
		h.Set("Access-Control-Allow-Headers", corsAllowHeaders)
		h.Set("Access-Control-Max-Age", "86400")
		w.WriteHeader(http.StatusNoContent)
		return true
	}
	return false
}

func (s *Server) handler(r *chi.Mux) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		// At debug level every call is logged (path only: queries may carry tokens),
		// which is how a client's needs are diagnosed.
		w := rw
		if s.Log != nil && s.Log.Enabled(req.Context(), slog.LevelDebug) {
			rec := &statusRecorder{ResponseWriter: rw, status: http.StatusOK}
			w = rec
			defer func() {
				s.Log.Debug("compat request", "method", req.Method, "path", req.URL.Path, "status", rec.status,
					"client", clientInfo(req).name, "params", paramNames(req), "authHeader", authHeaderNames(req))
			}()
		}
		if cors(w, req) {
			return
		}
		// Signed media URLs (HLS, subtitles) handed out in PlaybackInfo are relative
		// to the client's server address, which includes this prefix.
		if strings.HasPrefix(req.URL.Path, "/stream/") && s.Stream != nil {
			rest := strings.TrimPrefix(req.URL.Path, "/stream")
			if rctx := chi.RouteContext(req.Context()); rctx != nil {
				rctx.RoutePath = rest
			}
			http.StripPrefix("/stream", s.Stream.Routes()).ServeHTTP(w, req)
			return
		}
		r.ServeHTTP(w, req)
	})
}

// paramNames lists the query parameter names of a request (never their values).
func paramNames(r *http.Request) []string {
	var out []string
	for k := range r.URL.Query() {
		out = append(out, k)
	}
	return out
}

// authHeaderNames lists which credential headers a request carries (never their values).
func authHeaderNames(r *http.Request) []string {
	var out []string
	for _, h := range []string{"Authorization", "X-Emby-Authorization", "X-Emby-Token", "X-MediaBrowser-Token"} {
		if r.Header.Get(h) != "" {
			out = append(out, h)
		}
	}
	return out
}

// statusRecorder remembers the response status for the debug log.
type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (r *statusRecorder) WriteHeader(code int) {
	r.status = code
	r.ResponseWriter.WriteHeader(code)
}

// Unwrap lets http.ResponseController reach the underlying writer (flush, deadlines).
func (r *statusRecorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// lowercasePath makes routing case-insensitive (clients send "/Users/AuthenticateByName").
// Path values are hex IDs, so lowercasing them is harmless.
func lowercasePath(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if rctx := chi.RouteContext(r.Context()); rctx != nil && rctx.RoutePath != "" {
			rctx.RoutePath = strings.ToLower(rctx.RoutePath)
		} else {
			r.URL.Path = strings.ToLower(r.URL.Path)
			r.URL.RawPath = ""
		}
		next.ServeHTTP(w, r)
	})
}

// --- auth -------------------------------------------------------------------

type ctxKey struct{}

// client is what a caller says about itself in the MediaBrowser authorization header.
type client struct {
	token, name, device, deviceID, version string
}

var authFieldRe = regexp.MustCompile(`(?i)\b(token|client|device|deviceid|version)="([^"]*)"`)

// clientInfo parses `Authorization: MediaBrowser Client="…", Device="…", Token="…"`
// (or X-Emby-Authorization, X-Emby-Token, api_key, Bearer).
func clientInfo(r *http.Request) client {
	h := r.Header.Get("Authorization")
	if h == "" {
		h = r.Header.Get("X-Emby-Authorization")
	}
	var c client
	for _, m := range authFieldRe.FindAllStringSubmatch(h, -1) {
		switch strings.ToLower(m[1]) {
		case "token":
			c.token = m[2]
		case "client":
			c.name = m[2]
		case "device":
			c.device = m[2]
		case "deviceid":
			c.deviceID = m[2]
		case "version":
			c.version = m[2]
		}
	}
	if c.token == "" {
		for _, k := range []string{"X-Emby-Token", "X-MediaBrowser-Token"} {
			if v := r.Header.Get(k); v != "" {
				c.token = v
			}
		}
	}
	if c.token == "" {
		c.token = param(r, "api_key")
		if c.token == "" {
			c.token = param(r, "ApiKey")
		}
	}
	if c.token == "" && strings.HasPrefix(h, "Bearer ") {
		c.token = strings.TrimPrefix(h, "Bearer ")
	}
	return c
}

func (s *Server) requireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p, err := s.Auth.Authenticate(r.Context(), clientInfo(r).token)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, p)))
	})
}

func principal(r *http.Request) auth.Principal {
	p, _ := r.Context().Value(ctxKey{}).(auth.Principal)
	return p
}

// --- helpers ----------------------------------------------------------------

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) name(ctx context.Context) string {
	if s.ServerName != nil {
		return s.ServerName(ctx)
	}
	return "Streamero"
}

// param reads a query parameter case-insensitively.
func param(r *http.Request, name string) string {
	for k, v := range r.URL.Query() {
		if strings.EqualFold(k, name) && len(v) > 0 {
			return v[0]
		}
	}
	return ""
}

func ownerOf(p auth.Principal) uuid.UUID {
	if p.Profile != nil {
		return p.Profile.ID
	}
	return p.User.ID
}

func canSee(p auth.Principal) bool { return p.User.IsAdmin || p.User.Permissions.AllLibraries }

// --- system & users -----------------------------------------------------------

func (s *Server) publicInfo(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"LocalAddress": "", "ServerName": s.name(r.Context()), "Version": APIVersion,
		"ProductName": ProductName, "OperatingSystem": "", "Id": ID(s.ServerID), "StartupWizardCompleted": true,
	})
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ServerName": s.name(r.Context()), "Version": APIVersion, "ProductName": ProductName, "Id": ID(s.ServerID),
		"StartupWizardCompleted": true, "OperatingSystem": "Linux", "LocalAddress": "",
		"HasPendingRestart": false, "IsShuttingDown": false, "SupportsLibraryMonitor": true, "WebSocketPortNumber": 0,
		"CanSelfRestart": false, "CanLaunchWebBrowser": false, "HasUpdateAvailable": false,
		"CompletedInstallations": []any{}, "CastReceiverApplications": []any{},
	})
}

func (s *Server) ping(w http.ResponseWriter, r *http.Request) { writeJSON(w, s.name(r.Context())) }

// configuration is the minimal server configuration object. Sonarr fetches it
// to test the connection; nothing in it is used by clients.
func (s *Server) configuration(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, map[string]any{
		"ServerName": s.name(r.Context()), "IsStartupWizardCompleted": true,
		"UICulture": "en-US", "PreferredMetadataLanguage": "en", "MetadataCountryCode": "US",
	})
}

func endpointInfo(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"IsLocal": false, "IsInNetwork": false})
}

func branding(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, map[string]any{"LoginDisclaimer": "", "CustomCss": "", "SplashscreenEnabled": false})
}

func brandingCSS(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/css; charset=utf-8")
}

// displayPreferences answers defaults; clients keep working without stored preferences.
func displayPreferences(w http.ResponseWriter, r *http.Request) {
	client := param(r, "Client")
	writeJSON(w, map[string]any{"Id": chi.URLParam(r, "id"), "SortBy": "SortName", "SortOrder": "Ascending",
		"RememberIndexing": false, "RememberSorting": false, "PrimaryImageHeight": 250, "PrimaryImageWidth": 250,
		"CustomPrefs": map[string]string{}, "ScrollDirection": "Horizontal", "ShowBackdrop": true, "ShowSidebar": false,
		"Client": client, "IndexBy": nil})
}

// accepted acknowledges requests Streamero has no use for (e.g. admin
// notifications from *arr apps, client capabilities), so senders do not report failures.
func accepted(w http.ResponseWriter, r *http.Request) {
	_, _ = io.Copy(io.Discard, http.MaxBytesReader(w, r.Body, 1<<20))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) userDTO(u auth.User) map[string]any {
	last := ""
	if u.LastLoginAt != nil {
		last = u.LastLoginAt.UTC().Format(time.RFC3339)
	}
	perms := u.Permissions
	all := u.IsAdmin
	return map[string]any{
		"Name": u.Username, "ServerId": ID(s.ServerID), "ServerName": "", "Id": ID(u.ID), "HasPassword": true,
		"HasConfiguredPassword": true, "HasConfiguredEasyPassword": false, "EnableAutoLogin": false,
		"LastLoginDate": last, "LastActivityDate": last,
		"Policy": map[string]any{
			"IsAdministrator": u.IsAdmin, "IsDisabled": u.Disabled, "IsHidden": true,
			"EnableAllFolders": all || perms.AllLibraries, "EnabledFolders": []string{},
			"EnableCollectionManagement": all || perms.ManageCollections, "EnableSubtitleManagement": all || perms.EditSubtitles,
			"EnableLyricManagement": false, "EnableUserPreferenceAccess": true,
			"EnableRemoteControlOfOtherUsers": false, "EnableSharedDeviceControl": false, "EnableRemoteAccess": true,
			"EnableLiveTvManagement": all || perms.ManageLiveTV, "EnableLiveTvAccess": all || perms.LiveTV, "EnableMediaPlayback": true,
			"EnableAudioPlaybackTranscoding": all || perms.AudioTranscoding,
			"EnableVideoPlaybackTranscoding": all || perms.VideoTranscoding, "EnablePlaybackRemuxing": all || perms.Remux,
			"ForceRemoteSourceTranscoding": false, "EnableContentDeletion": false,
			"EnableContentDownloading": all || perms.Download, "EnableSyncTranscoding": false, "EnableMediaConversion": false,
			"EnableAllDevices": true, "EnableAllChannels": false, "EnablePublicSharing": false,
			"InvalidLoginAttemptCount": 0, "LoginAttemptsBeforeLockout": auth.MaxFailedLogins,
			"MaxActiveSessions": perms.MaxSessions, "RemoteClientBitrateLimit": 0,
			"AuthenticationProviderId": "Streamero", "PasswordResetProviderId": "Streamero",
			"SyncPlayAccess": syncPlayAccess(all || perms.SyncPlay), "BlockedTags": []string{}, "AllowedTags": []string{},
			"BlockUnratedItems": []string{}, "EnabledDevices": []string{}, "EnabledChannels": []string{},
			"BlockedMediaFolders": []string{}, "BlockedChannels": []string{}, "AccessSchedules": []any{},
		},
		"Configuration": map[string]any{
			"PlayDefaultAudioTrack": true, "DisplayMissingEpisodes": false, "GroupedFolders": []string{},
			"SubtitleMode": "Default", "DisplayCollectionsView": false, "EnableLocalPassword": false,
			"OrderedViews": []string{}, "LatestItemsExcludes": []string{}, "MyMediaExcludes": []string{},
			"HidePlayedInLatest": true, "RememberAudioSelections": true, "RememberSubtitleSelections": true,
			"EnableNextEpisodeAutoPlay": true, "AudioLanguagePreference": "", "SubtitleLanguagePreference": "",
		},
	}
}

// authResult is the sign-in answer (user, token and the new session).
func (s *Server) authResult(p auth.Principal, token string, c client) map[string]any {
	now := time.Now().UTC().Format(time.RFC3339)
	return map[string]any{
		"User": s.userDTO(p.User), "AccessToken": token, "ServerId": ID(s.ServerID),
		"SessionInfo": map[string]any{"Id": ID(p.SessionID), "UserId": ID(p.User.ID), "UserName": p.User.Username,
			"Client": c.name, "DeviceName": c.device, "DeviceId": c.deviceID, "ApplicationVersion": c.version,
			"ServerId": ID(s.ServerID), "PlayableMediaTypes": []string{"Video", "Audio"}, "LastActivityDate": now,
			"LastPlaybackCheckIn": now, "IsActive": true, "SupportsMediaControl": false, "SupportsRemoteControl": false,
			"HasCustomDeviceName": false, "SupportedCommands": []string{}, "AdditionalUsers": []any{},
			"NowPlayingQueue": []any{}, "NowPlayingQueueFullItems": []any{}},
	}
}

func (c client) authDevice() auth.Device {
	name := c.name
	if name == "" {
		name = "Compat client"
	}
	return auth.Device{Name: c.device, Client: name}
}

func (s *Server) authenticateByName(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Username string `json:"Username"`
		Pw       string `json:"Pw"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	c := clientInfo(r)
	p, token, err := s.Auth.Login(r.Context(), body.Username, body.Pw, "", c.authDevice())
	if err != nil {
		var locked *auth.LockedError
		switch {
		case errors.As(err, &locked):
			http.Error(w, "account locked", http.StatusTooManyRequests)
		case errors.Is(err, auth.ErrPasswordLoginDisabled):
			http.Error(w, "password sign-in is turned off on this server: use Quick Connect and approve the code in "+
				"Streamero (Settings → Quick Connect)", http.StatusUnauthorized)
		case errors.Is(err, auth.ErrTOTPRequired):
			http.Error(w, "this account uses two-factor sign-in: use Quick Connect and approve the code in "+
				"Streamero (Settings → Quick Connect), or use an API key", http.StatusUnauthorized)
		default:
			http.Error(w, "invalid username or password", http.StatusUnauthorized)
		}
		return
	}
	writeJSON(w, s.authResult(p, token, c))
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	_ = s.Auth.Logout(r.Context(), principal(r))
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, s.userDTO(principal(r).User))
}

func (s *Server) users(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		writeJSON(w, []any{s.userDTO(p.User)})
		return
	}
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	out := make([]any, 0, len(users))
	for _, u := range users {
		out = append(out, s.userDTO(u))
	}
	writeJSON(w, out)
}

func (s *Server) user(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	id, err := ParseID(chi.URLParam(r, "userId"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	if id == p.User.ID {
		writeJSON(w, s.userDTO(p.User))
		return
	}
	if !p.User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	users, err := s.Auth.ListUsers(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	for _, u := range users {
		if u.ID == id {
			writeJSON(w, s.userDTO(u))
			return
		}
	}
	http.NotFound(w, r)
}

// --- Quick Connect ------------------------------------------------------------------

func quickConnectEnabled(w http.ResponseWriter, _ *http.Request) { writeJSON(w, true) }

func quickConnectResult(secret string, st auth.QuickConnectState, c client) map[string]any {
	return map[string]any{"Authenticated": st.Approved, "Secret": secret, "Code": st.Code, "DeviceId": c.deviceID,
		"DeviceName": st.DeviceName, "AppName": st.Client, "AppVersion": c.version,
		"DateAdded": st.CreatedAt.UTC().Format(time.RFC3339)}
}

// quickConnectInitiate starts a request; the user approves the code in Streamero
// (Settings → Quick Connect) or from a signed-in client. Rate limited by IP.
func (s *Server) quickConnectInitiate(w http.ResponseWriter, r *http.Request) {
	c := clientInfo(r)
	dev := c.authDevice()
	st, err := s.Auth.InitiateQuickConnect(r.Context(), dev)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, quickConnectResult(st.Secret, auth.QuickConnectState{Code: st.Code, DeviceName: dev.Name, Client: dev.Client,
		CreatedAt: time.Now()}, c))
}

// quickConnectConnect reports whether the code was approved (the client polls it).
func (s *Server) quickConnectConnect(w http.ResponseWriter, r *http.Request) {
	secret := param(r, "Secret")
	st, err := s.Auth.QuickConnectStatus(r.Context(), secret)
	if errors.Is(err, auth.ErrNotFound) {
		http.Error(w, "unknown or expired quick connect request", http.StatusNotFound)
		return
	}
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, quickConnectResult(secret, st, clientInfo(r)))
}

// quickConnectAuthenticate signs the device in once its code was approved.
// Two-factor accounts sign in this way: the approving session already passed 2FA.
func (s *Server) quickConnectAuthenticate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Secret string `json:"Secret"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(&body); err != nil || body.Secret == "" {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	p, token, err := s.Auth.PollQuickConnect(r.Context(), body.Secret)
	switch {
	case errors.Is(err, auth.ErrQuickConnectPending):
		http.Error(w, "quick connect code not approved yet", http.StatusUnauthorized)
		return
	case errors.Is(err, auth.ErrNotFound):
		http.Error(w, "unknown or expired quick connect request", http.StatusUnauthorized)
		return
	case err != nil:
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, s.authResult(p, token, clientInfo(r)))
}

// quickConnectAuthorize approves a code from a signed-in client (not an API key).
func (s *Server) quickConnectAuthorize(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if p.SessionID == uuid.Nil || p.IsKid() {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	if _, err := s.Auth.AuthorizeQuickConnect(r.Context(), p, param(r, "Code")); err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			http.Error(w, "unknown or expired code", http.StatusNotFound)
			return
		}
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	writeJSON(w, true)
}

// --- libraries ------------------------------------------------------------------------

func collectionType(kind string) string {
	switch kind {
	case "movies":
		return "movies"
	case "shows":
		return "tvshows"
	case "music":
		return "music"
	}
	return "mixed"
}

func (s *Server) viewDTO(l library.Library) map[string]any {
	return map[string]any{"Name": l.Name, "Id": ID(l.ID), "ServerId": ID(s.ServerID), "Etag": ID(l.ID)[:16],
		"Type": "CollectionFolder", "CollectionType": collectionType(l.Kind), "IsFolder": true, "MediaType": "Unknown",
		"ChildCount": l.ItemCount, "LocationType": "FileSystem", "DisplayPreferencesId": ID(l.ID),
		"ImageTags": map[string]string{}, "BackdropImageTags": []string{}, "SortName": strings.ToLower(l.Name),
		"UserData": map[string]any{"PlaybackPositionTicks": 0, "PlayCount": 0, "IsFavorite": false, "Played": false,
			"Key": ID(l.ID), "ItemId": ID(l.ID)}}
}

func (s *Server) views(w http.ResponseWriter, r *http.Request) {
	out := []any{}
	if canSee(principal(r)) {
		libs, err := s.Libraries.List(r.Context())
		if err != nil {
			http.Error(w, "error", http.StatusInternalServerError)
			return
		}
		for _, l := range libs {
			if library.IsStreamingLibrary(l.Kind) {
				out = append(out, s.viewDTO(l))
			}
		}
	}
	writeJSON(w, queryResult(out, len(out), 0))
}

// --- library change notifications (*arr "Emby/Jellyfin" connection) ---------

func (s *Server) refreshAll(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	libs, err := s.Libraries.List(r.Context())
	if err == nil {
		for _, l := range libs {
			_ = s.Libraries.Scan(r.Context(), l.ID)
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// mediaUpdated scans the libraries containing the reported paths (or all when
// the paths are unknown, e.g. the sender sees a different mount point).
func (s *Server) mediaUpdated(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	var body struct {
		Updates []struct {
			Path string `json:"Path"`
		} `json:"Updates"`
	}
	_ = json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&body)
	libs, err := s.Libraries.List(r.Context())
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	queued := map[uuid.UUID]bool{}
	for _, u := range body.Updates {
		p := filepath.Clean(u.Path)
		for _, l := range libs {
			for _, root := range l.Paths {
				if p == root || strings.HasPrefix(p, root+string(filepath.Separator)) {
					queued[l.ID] = true
				}
			}
		}
	}
	if len(queued) == 0 { // unknown paths (different mount points): scan everything
		for _, l := range libs {
			queued[l.ID] = true
		}
	}
	for id := range queued {
		_ = s.Libraries.Scan(r.Context(), id)
	}
	s.Log.Info("library update notification", "libraries", len(queued), "paths", len(body.Updates))
	w.WriteHeader(http.StatusNoContent)
}

// webRedirect turns "…/web/#/details?id=<32 hex>" links (as built by request
// managers for "Play on") into the Streamero item page. The id is read in the
// browser because URL fragments never reach the server.
func webRedirect(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; script-src 'sha256-"+webRedirectHash+"'")
	_, _ = w.Write([]byte(webRedirectHTML))
}

const webRedirectScript = `var m=(location.hash+location.search).match(/[?&]id=([0-9a-fA-F-]{32,36})/);` +
	`if(m){var h=m[1].replace(/-/g,"").toLowerCase();` +
	`location.replace("/item/"+h.slice(0,8)+"-"+h.slice(8,12)+"-"+h.slice(12,16)+"-"+h.slice(16,20)+"-"+h.slice(20));}` +
	`else{location.replace("/");}`

// webRedirectHash allows exactly this inline script in the CSP.
var webRedirectHash = func() string {
	sum := sha256.Sum256([]byte(webRedirectScript))
	return base64.StdEncoding.EncodeToString(sum[:])
}()

const webRedirectHTML = `<!doctype html><meta charset="utf-8"><title>Streamero</title><script>` + webRedirectScript + `</script>`

// createKey implements POST /Auth/Keys?App=<name> (admin): an integration API key.
func (s *Server) createKey(w http.ResponseWriter, r *http.Request) {
	p := principal(r)
	if !p.User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	app := strings.TrimSpace(param(r, "App"))
	if app == "" {
		app = "Integration"
	}
	_, secret, err := s.Auth.CreateAPIKey(r.Context(), p, app)
	if err != nil {
		http.Error(w, "error", http.StatusInternalServerError)
		return
	}
	s.keysMu.Lock()
	now := time.Now()
	kept := s.keys[:0]
	for _, k := range s.keys {
		if now.Sub(k.created) < recentKeyTTL {
			kept = append(kept, k)
		}
	}
	s.keys = append(kept, recentKey{app: app, secret: secret, created: now})
	s.keysMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// listKeys implements GET /Auth/Keys (admin): keys created here in the last minutes.
func (s *Server) listKeys(w http.ResponseWriter, r *http.Request) {
	if !principal(r).User.IsAdmin {
		http.Error(w, "forbidden", http.StatusForbidden)
		return
	}
	s.keysMu.Lock()
	out := []any{}
	now := time.Now()
	for _, k := range s.keys {
		if now.Sub(k.created) < recentKeyTTL {
			out = append(out, map[string]any{"AccessToken": k.secret, "AppName": k.app,
				"DateCreated": k.created.UTC().Format(time.RFC3339), "IsActive": true})
		}
	}
	s.keysMu.Unlock()
	writeJSON(w, map[string]any{"Items": out, "TotalRecordCount": len(out), "StartIndex": 0})
}

func syncPlayAccess(allowed bool) string {
	if allowed {
		return "CreateAndJoinGroups"
	}
	return "None"
}
