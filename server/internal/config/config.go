// Package config loads process configuration from the environment.
// Settings that admins change at runtime live in the database, not here.
package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Config is the static process configuration.
type Config struct {
	HTTPAddr       string
	DatabaseURL    string
	ValkeyURL      string
	ConfigDir      string
	BackupDir      string // where backups are written (default <ConfigDir>/backups)
	CacheDir       string
	TranscodeDir   string
	ServerName     string
	SetupCode      string // optional fixed first-run setup code (otherwise random per start)
	TrustedProxies string // CIDRs/IPs whose X-Forwarded-For is trusted, comma-separated
	MediaRoots     string // folders libraries may use, comma-separated
	FFprobePath    string
	FFmpegPath     string
	AuthRatePerMin int // sign-in style requests per client IP per minute (0 = no limit)
	AuthRateBurst  int
	AutoMigrate    bool
	// PasswordLoginRecovery lets administrators sign in with a password while
	// password sign-in is turned off for single sign-on (D-037, docs/SSO.md).
	PasswordLoginRecovery bool
	// CompatAtRoot also serves the compat API at Jellyfin-style root paths (D-032).
	CompatAtRoot bool
	// Discovery answers the media clients' LAN discovery broadcast (D-033).
	Discovery     bool
	DiscoveryAddr string // UDP listen address, default ":7359"
	// DiscoveryURL is the server address announced to discovering clients
	// (default: http://<address of the interface facing the client>:<HTTP port>).
	DiscoveryURL    string
	LogLevel        string
	ShutdownTimeout time.Duration
}

// Load reads the configuration from environment variables.
func Load() (Config, error) {
	c := Config{
		HTTPAddr:        env("STREAMERO_HTTP_ADDR", ":8787"),
		DatabaseURL:     os.Getenv("STREAMERO_DB_URL"),
		ValkeyURL:       env("STREAMERO_VALKEY_URL", "redis://localhost:6379/0"),
		ConfigDir:       env("STREAMERO_CONFIG_DIR", "/config"),
		CacheDir:        env("STREAMERO_CACHE_DIR", "/cache"),
		TranscodeDir:    env("STREAMERO_TRANSCODE_DIR", "/transcode"),
		ServerName:      env("STREAMERO_SERVER_NAME", "Streamero"),
		SetupCode:       os.Getenv("STREAMERO_SETUP_CODE"),
		TrustedProxies:  os.Getenv("STREAMERO_TRUSTED_PROXIES"),
		MediaRoots:      env("STREAMERO_MEDIA_ROOTS", "/media"),
		FFprobePath:     env("STREAMERO_FFPROBE", "ffprobe"),
		FFmpegPath:      env("STREAMERO_FFMPEG", "ffmpeg"),
		LogLevel:        env("STREAMERO_LOG_LEVEL", "info"),
		ShutdownTimeout: 15 * time.Second,
	}
	c.BackupDir = env("STREAMERO_BACKUP_DIR", strings.TrimRight(c.ConfigDir, "/")+"/backups")
	var err error
	if c.AutoMigrate, err = envBool("STREAMERO_AUTO_MIGRATE", true); err != nil {
		return c, err
	}
	if c.PasswordLoginRecovery, err = envBool("STREAMERO_PASSWORD_LOGIN_RECOVERY", false); err != nil {
		return c, err
	}
	if c.CompatAtRoot, err = envBool("STREAMERO_COMPAT_ROOT", true); err != nil {
		return c, err
	}
	if c.Discovery, err = envBool("STREAMERO_DISCOVERY", true); err != nil {
		return c, err
	}
	c.DiscoveryAddr = env("STREAMERO_DISCOVERY_ADDR", ":7359")
	c.DiscoveryURL = strings.TrimRight(os.Getenv("STREAMERO_DISCOVERY_URL"), "/")
	if c.AuthRatePerMin, c.AuthRateBurst, err = parseRate(env("STREAMERO_AUTH_RATE_LIMIT", "20,10")); err != nil {
		return c, err
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("STREAMERO_DB_URL is required")
	}
	return c, nil
}

// parseRate reads "perMinute,burst" (or "0" to disable).
func parseRate(s string) (int, int, error) {
	if strings.TrimSpace(s) == "0" {
		return 0, 0, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("STREAMERO_AUTH_RATE_LIMIT: want \"perMinute,burst\" or \"0\", got %q", s)
	}
	per, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
	burst, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil || per <= 0 || burst <= 0 {
		return 0, 0, fmt.Errorf("STREAMERO_AUTH_RATE_LIMIT: invalid value %q", s)
	}
	return per, burst, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

func envBool(key string, def bool) (bool, error) {
	v, ok := os.LookupEnv(key)
	if !ok || v == "" {
		return def, nil
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return def, fmt.Errorf("%s: %w", key, err)
	}
	return b, nil
}
