// Package config reads the server's environment.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	// Port the HTTP server listens on (PORT, default 3000).
	Port int

	// DecksDir is the presentations folder (DECKS_DIR, required; the image
	// sets /decks). It is watched: changes are picked up without a restart.
	DecksDir string

	// ReloadInterval is how often DecksDir is checked for changes
	// (RELOAD_INTERVAL, Go duration, default 5s; 1s in dev). SIGHUP forces
	// an immediate check.
	ReloadInterval time.Duration

	// Dev (DEV=1) serves `draft: true` decks and makes open decks reload
	// themselves when their source changes. For authoring, not for the
	// public server.
	Dev bool

	// ClientIPHeader names the request header that carries the real client
	// address (CLIENT_IP_HEADER). Behind Cloudflare / cloudflared this is
	// "CF-Connecting-IP". Empty means the TCP peer address — correct only
	// when nothing proxies in front. Only set it when the server is reachable
	// exclusively through that proxy: the header is trusted as-is.
	ClientIPHeader string

	// AppDir replaces the embedded frontend with a directory on disk
	// (APP_DIR) — for frontend development.
	AppDir string
}

func (c Config) Addr() string { return fmt.Sprintf(":%d", c.Port) }

func Load() (Config, error) {
	c := Config{
		Port:           3000,
		DecksDir:       os.Getenv("DECKS_DIR"),
		ClientIPHeader: os.Getenv("CLIENT_IP_HEADER"),
		AppDir:         os.Getenv("APP_DIR"),
	}
	if p := os.Getenv("PORT"); p != "" {
		n, err := strconv.Atoi(p)
		if err != nil || n < 1 || n > 65535 {
			return c, fmt.Errorf("PORT: %q is not a valid port", p)
		}
		c.Port = n
	}
	if v := os.Getenv("DEV"); v != "" {
		dev, err := strconv.ParseBool(v)
		if err != nil {
			return c, fmt.Errorf("DEV: %q is not a boolean", v)
		}
		c.Dev = dev
	}
	c.ReloadInterval = 5 * time.Second
	if c.Dev {
		c.ReloadInterval = time.Second
	}
	if v := os.Getenv("RELOAD_INTERVAL"); v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d < 100*time.Millisecond {
			return c, fmt.Errorf("RELOAD_INTERVAL: %q is not a duration of at least 100ms", v)
		}
		c.ReloadInterval = d
	}
	return c, nil
}

// Validate is separate from Load so `slides healthcheck` works without the
// serving settings.
func (c Config) Validate() error {
	if c.DecksDir == "" {
		return errors.New("DECKS_DIR is not set: point it at the presentations folder (the image expects a mount at /decks)")
	}
	info, err := os.Stat(c.DecksDir)
	if err != nil {
		return fmt.Errorf("DECKS_DIR %s: %w — is the presentations folder mounted?", c.DecksDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("DECKS_DIR %s is not a directory", c.DecksDir)
	}
	return nil
}
