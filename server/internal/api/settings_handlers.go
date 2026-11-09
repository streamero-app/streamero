package api

import (
	"context"
	"net/http"
	"strings"

	"github.com/streamero-app/streamero/server/internal/api/gen"
	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// KeyBranding is the settings key of the login message and custom CSS.
const KeyBranding = "branding"

// Branding is what an administrator adds to the web client.
type Branding struct {
	LoginDisclaimer string `json:"loginDisclaimer"`
	CustomCSS       string `json:"customCss"`
}

// LoadEncoding returns the transcoding settings (defaults when unset or unreadable).
func (h *Handler) LoadEncoding(ctx context.Context) stream.Encoding {
	e := stream.DefaultEncoding()
	if h.Settings != nil && h.Settings() != nil {
		_, _ = h.Settings().Get(ctx, stream.KeyEncoding, &e)
	}
	return e.Normalize()
}

// LoadResume returns the resume thresholds (defaults when unset).
func (h *Handler) LoadResume(ctx context.Context) library.Resume {
	r := library.DefaultResume()
	if h.Settings != nil && h.Settings() != nil {
		_, _ = h.Settings().Get(ctx, library.KeyResume, &r)
	}
	return r
}

func (h *Handler) loadBranding(ctx context.Context) Branding {
	var b Branding
	if h.Settings != nil && h.Settings() != nil {
		_, _ = h.Settings().Get(ctx, KeyBranding, &b)
	}
	return b
}

func toEncoding(e stream.Encoding) *gen.EncodingSettings {
	return &gen.EncodingSettings{HardwareAcceleration: gen.EncodingSettingsHardwareAcceleration(e.HWAccel), VaapiDevice: gen.NewOptString(e.VAAPIDevice),
		Preset: gen.EncodingSettingsPreset(e.Preset), Quality: e.Quality, AudioBitrateKbps: e.AudioKbps, SegmentSeconds: e.SegmentSeconds,
		MaxSessions: e.MaxSessions, ToneMapping: e.ToneMaps()}
}

// GetEncodingSettings implements GET /settings/encoding.
func (h *Handler) GetEncodingSettings(ctx context.Context) (*gen.EncodingSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	return toEncoding(h.LoadEncoding(ctx)), nil
}

// UpdateEncodingSettings implements PUT /settings/encoding.
func (h *Handler) UpdateEncodingSettings(ctx context.Context, req *gen.EncodingSettings) (*gen.EncodingSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	dev := strings.TrimSpace(req.VaapiDevice.Value)
	if dev != "" && !strings.HasPrefix(dev, "/dev/dri/") {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "the VAAPI device must be under /dev/dri/"}
	}
	tm := req.ToneMapping
	e := stream.Encoding{HWAccel: string(req.HardwareAcceleration), VAAPIDevice: dev, Preset: string(req.Preset), Quality: req.Quality,
		AudioKbps: req.AudioBitrateKbps, SegmentSeconds: req.SegmentSeconds, MaxSessions: req.MaxSessions, ToneMapping: &tm}.Normalize()
	if err := h.Settings().Set(ctx, stream.KeyEncoding, e); err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "settings.encoding", map[string]any{"hardware": e.HWAccel})
	return toEncoding(e), nil
}

// GetResumeSettings implements GET /settings/resume.
func (h *Handler) GetResumeSettings(ctx context.Context) (*gen.ResumeSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	r := h.LoadResume(ctx)
	return &gen.ResumeSettings{MinResumePercent: r.MinPercent, MaxResumePercent: r.MaxPercent, MinResumeDurationSeconds: r.MinDurationSeconds}, nil
}

// UpdateResumeSettings implements PUT /settings/resume.
func (h *Handler) UpdateResumeSettings(ctx context.Context, req *gen.ResumeSettings) (*gen.ResumeSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	if req.MinResumePercent >= req.MaxResumePercent {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "the minimum must be below the maximum"}
	}
	r := library.Resume{MinPercent: req.MinResumePercent, MaxPercent: req.MaxResumePercent, MinDurationSeconds: req.MinResumeDurationSeconds}
	if err := h.Settings().Set(ctx, library.KeyResume, r); err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "settings.resume", nil)
	return req, nil
}

// GetBrandingSettings implements GET /settings/branding.
func (h *Handler) GetBrandingSettings(ctx context.Context) (*gen.BrandingSettings, error) {
	if _, err := admin(ctx); err != nil {
		return nil, err
	}
	b := h.loadBranding(ctx)
	return &gen.BrandingSettings{LoginDisclaimer: b.LoginDisclaimer, CustomCss: b.CustomCSS}, nil
}

// UpdateBrandingSettings implements PUT /settings/branding.
func (h *Handler) UpdateBrandingSettings(ctx context.Context, req *gen.BrandingSettings) (*gen.BrandingSettings, error) {
	p, err := admin(ctx)
	if err != nil {
		return nil, err
	}
	css := req.CustomCss
	// The CSS goes into a <style> element: it must not be able to close it.
	if strings.Contains(strings.ToLower(css), "</style") {
		return nil, &StatusError{Status: http.StatusBadRequest, Code: "invalid_request", Message: "custom CSS cannot contain </style>"}
	}
	b := Branding{LoginDisclaimer: strings.TrimSpace(req.LoginDisclaimer), CustomCSS: css}
	if err := h.Settings().Set(ctx, KeyBranding, b); err != nil {
		return nil, err
	}
	_ = h.Auth().Audit(ctx, &p.User.ID, "settings.branding", nil)
	return &gen.BrandingSettings{LoginDisclaimer: b.LoginDisclaimer, CustomCss: b.CustomCSS}, nil
}
