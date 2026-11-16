// Package arr is the administrator-only gateway to the *arr stack (Radarr,
// Sonarr, Lidarr, Prowlarr, Bazarr) and Seerr for the Streamero Manage apps
// (D-048). The apps never talk to those services directly: every call goes
// through this server, which adds the stored API key, so the keys never leave
// the server and the services need not be reachable from the internet.
//
// Only the public HTTP APIs of those apps are used (their published API
// documentation); nothing here is derived from their source code.
package arr

import (
	"net/http"
	"strings"

	"github.com/streamero-app/streamero/server/internal/requests"
)

// Service describes one connectable app.
type Service struct {
	ID   string // radarr | sonarr | lidarr | prowlarr | bazarr | seerr
	Name string
	// APIBase is the path prefix of the app's API under the proxy.
	APIBase string
	// StatusPath answers the version (relative to the base URL).
	StatusPath string
	// KeyHeader carries the API key.
	KeyHeader string
	// Images lists path prefixes (lower case, besides "api/") of artwork the app
	// serves outside its API; Files lists other such prefixes (backup downloads).
	Images, Files []string
	// Patch reports whether the app's API uses PATCH.
	Patch bool
}

// Services are all connectable apps, in the order the API lists them.
var Services = []Service{
	{ID: "radarr", Name: "Radarr", APIBase: "api/v3", StatusPath: "api/v3/system/status", KeyHeader: "X-Api-Key", Images: []string{"mediacover/"}, Files: []string{"backup/"}},
	{ID: "sonarr", Name: "Sonarr", APIBase: "api/v3", StatusPath: "api/v3/system/status", KeyHeader: "X-Api-Key", Images: []string{"mediacover/"}, Files: []string{"backup/"}},
	{ID: "lidarr", Name: "Lidarr", APIBase: "api/v1", StatusPath: "api/v1/system/status", KeyHeader: "X-Api-Key", Images: []string{"mediacover/"}, Files: []string{"backup/"}},
	{ID: "prowlarr", Name: "Prowlarr", APIBase: "api/v1", StatusPath: "api/v1/system/status", KeyHeader: "X-Api-Key", Files: []string{"backup/"}},
	{ID: "bazarr", Name: "Bazarr", APIBase: "api", StatusPath: "api/system/status", KeyHeader: "X-API-KEY"},
	{ID: "seerr", Name: "Seerr", APIBase: "api/v1", StatusPath: "api/v1/status", KeyHeader: "X-Api-Key", Images: []string{"imageproxy/"}, Patch: true},
}

// Lookup finds a service by id.
func Lookup(id string) (Service, bool) {
	for _, s := range Services {
		if s.ID == id {
			return s, true
		}
	}
	return Service{}, false
}

// Conn returns the stored base URL and API key of the service.
func (s Service) Conn(st requests.Settings) (base, key string) {
	switch s.ID {
	case "radarr":
		return st.RadarrURL, st.RadarrKey
	case "sonarr":
		return st.SonarrURL, st.SonarrKey
	case "lidarr":
		return st.LidarrURL, st.LidarrKey
	case "prowlarr":
		return st.ProwlarrURL, st.ProwlarrKey
	case "bazarr":
		return st.BazarrURL, st.BazarrKey
	case "seerr":
		return st.SeerrURL, st.SeerrKey
	}
	return "", ""
}

// AllowsMethod reports whether the proxy forwards the method to the service.
func (s Service) AllowsMethod(m string) bool {
	switch m {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete:
		return true
	case http.MethodPatch:
		return s.Patch
	}
	return false
}

// isImagePath reports whether a (validated) path is artwork rather than API data.
func (s Service) isImagePath(path string) bool {
	p := strings.ToLower(path)
	for _, e := range s.Images {
		if strings.HasPrefix(p, e) {
			return true
		}
	}
	return strings.HasPrefix(p, "api/v3/mediacover/") || strings.HasPrefix(p, "api/v1/mediacover/")
}
