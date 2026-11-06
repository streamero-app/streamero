package auth

import "encoding/json"

// Permissions apply to non-admin users (admins may do everything).
type Permissions struct {
	AllLibraries      bool `json:"allLibraries"`
	Download          bool `json:"download"`
	VideoTranscoding  bool `json:"videoTranscoding"`
	AudioTranscoding  bool `json:"audioTranscoding"`
	Remux             bool `json:"remux"`
	LiveTV            bool `json:"liveTv"`
	ManageLiveTV      bool `json:"manageLiveTv"`
	SyncPlay          bool `json:"syncPlay"`
	DeleteMedia       bool `json:"deleteMedia"`
	ManageCollections bool `json:"manageCollections"`
	EditSubtitles     bool `json:"editSubtitles"`
	RemoteControl     bool `json:"remoteControl"`
	MaxSessions       int  `json:"maxSessions"` // 0 = unlimited
}

// DefaultPermissions are what a new user gets: watch everything, nothing destructive.
func DefaultPermissions() Permissions {
	return Permissions{
		AllLibraries: true, Download: true, VideoTranscoding: true, AudioTranscoding: true, Remux: true,
		LiveTV: true, SyncPlay: true, RemoteControl: true,
	}
}

// parsePermissions overlays stored keys on the defaults, so permissions added
// in later versions get their default for existing users.
func parsePermissions(raw []byte) Permissions {
	p := DefaultPermissions()
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &p) // invalid JSON keeps defaults
	}
	if p.MaxSessions < 0 {
		p.MaxSessions = 0
	}
	return p
}
