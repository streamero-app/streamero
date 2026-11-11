package api

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/auth"
	"github.com/streamero-app/streamero/server/internal/dlna"
)

func (h *Handler) loadDLNA(ctx context.Context) dlna.Settings {
	var st dlna.Settings
	if h.Settings != nil && h.Settings() != nil {
		_, _ = h.Settings().Get(ctx, dlna.SettingsKey, &st)
	}
	return st.Normalize()
}

func (h *Handler) dlnaOverview(ctx context.Context, st dlna.Settings) (*gen.DlnaOverview, error) {
	out := &gen.DlnaOverview{Settings: gen.DlnaSettings{Enabled: st.Enabled, FriendlyName: st.FriendlyName,
		Language: gen.DlnaSettingsLanguage(st.Language)}, Users: []gen.DlnaUserChoice{}}
	if st.UserID != nil {
		out.Settings.UserId = gen.NewOptUUID(*st.UserID)
	}
	if st.ProfileID != nil {
		out.Settings.ProfileId = gen.NewOptUUID(*st.ProfileID)
	}
	if srv := h.DLNA.Load(); srv != nil {
		s := srv.GetStatus()
		_, v := srv.Current(ctx)
		out.Status = gen.DlnaStatus{Available: true, Running: s.HTTPUp && v != nil, HttpAddr: s.HTTPAddr, Ssdp: s.SSDPUp,
			Interfaces: s.Interfaces}
		if s.Error != "" {
			out.Status.Error = gen.NewOptString(s.Error)
		}
	}
	users, err := h.Auth().ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	for _, u := range users {
		if u.Disabled {
			continue
		}
		profiles, err := h.Auth().ProfilesOf(ctx, u.ID)
		if err != nil {
			return nil, err
		}
		c := gen.DlnaUserChoice{ID: u.ID, Username: u.Username, Profiles: []gen.DlnaUserChoiceProfilesItem{}}
		for _, p := range profiles {
			c.Profiles = append(c.Profiles, gen.DlnaUserChoiceProfilesItem{ID: p.ID, Name: p.Name, IsKid: p.IsKid})
		}
		out.Users = append(out.Users, c)
	}
	return out, nil
}

// GetDlnaSettings implements GET /settings/dlna.
func (h *Handler) GetDlnaSettings(ctx context.Context) (*gen.DlnaOverview, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return h.dlnaOverview(ctx, h.loadDLNA(ctx))
}

// UpdateDlnaSettings implements PUT /settings/dlna.
func (h *Handler) UpdateDlnaSettings(ctx context.Context, req *gen.DlnaSettings) (*gen.DlnaOverview, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	bad := func(msg string) error {
		return &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: msg}
	}
	st := dlna.Settings{Enabled: req.Enabled, FriendlyName: strings.TrimSpace(req.FriendlyName), Language: string(req.Language)}
	if utf8.RuneCountInString(st.FriendlyName) > 64 {
		return nil, bad("the name can have at most 64 characters")
	}
	if v, ok := req.UserId.Get(); ok {
		st.UserID = &v
	}
	if v, ok := req.ProfileId.Get(); ok {
		if st.UserID == nil {
			return nil, bad("a profile needs its user")
		}
		st.ProfileID = &v
	}
	if st.Enabled && st.UserID == nil {
		return nil, bad("choose the user DLNA acts as before turning it on")
	}
	if st.UserID != nil {
		if _, err := h.Auth().ActAs(ctx, *st.UserID, st.ProfileID); errors.Is(err, auth.ErrNotFound) {
			return nil, bad("unknown or disabled user, or a profile of another user")
		} else if err != nil {
			return nil, err
		}
	}
	st = st.Normalize()
	if err := h.Settings().Set(ctx, dlna.SettingsKey, st); err != nil {
		return nil, err
	}
	if srv := h.DLNA.Load(); srv != nil {
		srv.Reload()
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "settings.dlna", map[string]any{"enabled": st.Enabled})
	return h.dlnaOverview(ctx, st)
}
