// Package config loads process configuration from the environment.
// Settings that admins change at runtime live in the database, not here.
package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is the static process configuration.
type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	ValkeyURL       string
	ConfigDir       string
	CacheDir        string
	TranscodeDir    string
	ServerName      string
	SetupCode       string // optional fixed first-run setup code (otherwise random per start)
	TrustedProxies  string // CIDRs/IPs whose X-Forwarded-For is trusted, comma-separated
	AutoMigrate     bool
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
		LogLevel:        env("STREAMERO_LOG_LEVEL", "info"),
		ShutdownTimeout: 15 * time.Second,
	}
	var err error
	if c.AutoMigrate, err = envBool("STREAMERO_AUTO_MIGRATE", true); err != nil {
		return c, err
	}
	if c.DatabaseURL == "" {
		return c, fmt.Errorf("STREAMERO_DB_URL is required")
	}
	return c, nil
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
