package dlna

import (
	"github.com/google/uuid"
)

// SettingsKey is the settings key of the DLNA configuration.
const SettingsKey = "dlna"

// Settings is what an administrator configures. DLNA has no sign-in, so it
// acts as one chosen user (and optionally one of its profiles); without one it
// stays off.
type Settings struct {
	Enabled      bool       `json:"enabled"`
	FriendlyName string     `json:"friendlyName"`
	UserID       *uuid.UUID `json:"userId,omitempty"`
	ProfileID    *uuid.UUID `json:"profileId,omitempty"`
	Language     string     `json:"language"` // folder names: "en" or "hr"
}

// Normalize fills defaults.
func (s Settings) Normalize() Settings {
	if s.Language != "hr" {
		s.Language = "en"
	}
	return s
}

// Active reports whether DLNA should serve anything.
func (s Settings) Active() bool { return s.Enabled && s.UserID != nil }

// Folder names per language (the TV shows these; the web client's i18n does
// not reach it).
var folderNames = map[string]map[string]string{
	"en": {"az": "A–Z", "new": "Recently added", "genres": "Genres", "artists": "Artists", "albums": "Albums",
		"continue": "Continue watching", "resume": "Resume from %s", "season": "Season %d", "specials": "Specials"},
	"hr": {"az": "A–Ž", "new": "Nedavno dodano", "genres": "Žanrovi", "artists": "Izvođači", "albums": "Albumi",
		"continue": "Nastavi gledati", "resume": "Nastavi od %s", "season": "Sezona %d", "specials": "Posebne epizode"},
}

func folderName(lang, key string) string {
	if m, ok := folderNames[lang]; ok {
		return m[key]
	}
	return folderNames["en"][key]
}
