package livetv

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/streamero-app/streamero/server/internal/library"
	"github.com/streamero-app/streamero/server/internal/settings"
	"github.com/streamero-app/streamero/server/internal/stream"
)

// Errors for the API layer.
var (
	ErrNotFound           = errors.New("not found")
	ErrTunersBusy         = errors.New("every tuner of this channel's source is in use")
	ErrChannelUnavailable = errors.New("the channel could not be tuned")
	ErrRecordingDone      = errors.New("this recording is already finished")
	ErrForbidden          = errors.New("only administrators may change the post-processing command")
)

// InvalidError is a validation error whose message is shown to the user.
type InvalidError struct{ Msg string }

func (e *InvalidError) Error() string { return e.Msg }

func invalid(format string, a ...any) error { return &InvalidError{Msg: fmt.Sprintf(format, a...)} }

// Service is Live TV and the DVR. One instance runs in the serve process: it
// owns the tuners (live sessions and recordings share their limits).
type Service struct {
	Pool     *pgxpool.Pool
	Settings func() *settings.Store
	// Stream signs URLs; HLS runs the live sessions.
	Signer stream.Signer
	HLS    *stream.Manager
	FFmpeg string
	// FFprobe reads the codecs of M3U channels (once, then cached on the channel).
	FFprobe string
	// FileRoots are the folders playlist and guide files may be read from.
	FileRoots library.Roots
	// RecordingsDir is where recordings are written (STREAMERO_RECORDINGS_DIR).
	RecordingsDir string
	// Library creates and scans the recordings library (nil: no library).
	Library *library.Service
	// HTTP fetches playlists, guides, tuner APIs and images.
	HTTP *http.Client
	// ImageDir caches channel logos and programme images.
	ImageDir string
	// EnqueueGuideRefresh queues a guide refresh for the worker (nil: refresh in a goroutine).
	EnqueueGuideRefresh func(ctx context.Context, guideID uuid.UUID) error
	Log                 *slog.Logger
	Now                 func() time.Time

	mu        sync.Mutex
	leases    map[uuid.UUID][]*lease // per tuner source
	live      map[string]*liveSession
	recorders map[uuid.UUID]*recorder
	kick      chan struct{}
}

func (s *Service) now() time.Time {
	if s.Now != nil {
		return s.Now()
	}
	return time.Now()
}

func (s *Service) log() *slog.Logger {
	if s.Log != nil {
		return s.Log
	}
	return slog.Default()
}

func (s *Service) http() *http.Client {
	if s.HTTP != nil {
		return s.HTTP
	}
	return NewHTTPClient(2*time.Minute, false)
}

// KeySettings is the settings key of the DVR settings.
const KeySettings = "livetv"

// Retention is the opt-in clean-up of finished recordings.
type Retention struct {
	Enabled         bool `json:"enabled"`
	DeleteAfterDays int  `json:"deleteAfterDays"`
	KeepLast        int  `json:"keepLast"`
}

// PostProcess is the admin's command run after each recording (no shell).
type PostProcess struct {
	Command        string   `json:"command"`
	Args           []string `json:"args"`
	TimeoutMinutes int      `json:"timeoutMinutes"`
}

// Settings are the DVR and time-shift settings.
type Settings struct {
	PrePadding       int         `json:"prePaddingSeconds"`
	PostPadding      int         `json:"postPaddingSeconds"`
	TimeshiftMinutes int         `json:"timeshiftMinutes"`
	Retention        Retention   `json:"retention"`
	PostProcess      PostProcess `json:"postProcess"`
	LibraryID        *uuid.UUID  `json:"recordingsLibraryId,omitempty"`
}

// DefaultSettings are one minute early, two minutes late, a 30-minute buffer.
func DefaultSettings() Settings {
	return Settings{PrePadding: 60, PostPadding: 120, TimeshiftMinutes: 30,
		PostProcess: PostProcess{Args: []string{}, TimeoutMinutes: 120}}
}

// LoadSettings returns the settings (defaults for anything unset).
func (s *Service) LoadSettings(ctx context.Context) Settings {
	st := DefaultSettings()
	if s.Settings != nil && s.Settings() != nil {
		_, _ = s.Settings().Get(ctx, KeySettings, &st)
	}
	if st.TimeshiftMinutes <= 0 {
		st.TimeshiftMinutes = 30
	}
	if st.PostProcess.TimeoutMinutes <= 0 {
		st.PostProcess.TimeoutMinutes = 120
	}
	if st.PostProcess.Args == nil {
		st.PostProcess.Args = []string{}
	}
	return st
}

// SaveSettings validates and stores the settings. Only administrators may
// change the post-processing command (it runs a program on the server).
func (s *Service) SaveSettings(ctx context.Context, in Settings, isAdmin bool) (Settings, error) {
	cur := s.LoadSettings(ctx)
	if in.PrePadding < 0 || in.PrePadding > maxPrePadding || in.PostPadding < 0 || in.PostPadding > maxPostPadding {
		return cur, invalid("padding is out of range")
	}
	if in.TimeshiftMinutes < 1 || in.TimeshiftMinutes > 240 {
		return cur, invalid("the time-shift buffer must be 1 to 240 minutes")
	}
	if in.Retention.DeleteAfterDays < 0 || in.Retention.KeepLast < 0 {
		return cur, invalid("retention values must not be negative")
	}
	if in.PostProcess.Args == nil {
		in.PostProcess.Args = []string{}
	}
	if !samePostProcess(in.PostProcess, cur.PostProcess) {
		if !isAdmin {
			return cur, ErrForbidden
		}
		if err := validatePostProcess(in.PostProcess); err != nil {
			return cur, err
		}
	}
	in.LibraryID = cur.LibraryID
	if err := s.Settings().Set(ctx, KeySettings, in); err != nil {
		return cur, err
	}
	s.Kick()
	return s.LoadSettings(ctx), nil
}

func samePostProcess(a, b PostProcess) bool {
	if a.Command != b.Command || a.TimeoutMinutes != b.TimeoutMinutes || len(a.Args) != len(b.Args) {
		return false
	}
	for i := range a.Args {
		if a.Args[i] != b.Args[i] {
			return false
		}
	}
	return true
}

// Kick wakes the DVR loop (new or changed recordings, rules or settings).
func (s *Service) Kick() {
	s.mu.Lock()
	k := s.kick
	s.mu.Unlock()
	if k == nil {
		return
	}
	select {
	case k <- struct{}{}:
	default:
	}
}
