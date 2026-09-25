package config

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/spf13/viper"
)

// SourceType identifies the media server backend
type SourceType string

const (
	SourceTypePlex     SourceType = "plex"
	SourceTypeJellyfin SourceType = "jellyfin"
)

// Config holds all application configuration
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Player  PlayerConfig  `mapstructure:"player"`
	UI      UIConfig      `mapstructure:"ui"`
	Logging LoggingConfig `mapstructure:"logging"`

	v *viper.Viper // the instance Load read; Save writes through it
}

// ServerConfig holds media server configuration
type ServerConfig struct {
	Type     SourceType `mapstructure:"type"`      // "plex" or "jellyfin"
	URL      string     `mapstructure:"url"`       // Server URL
	Token    string     `mapstructure:"token"`     // Plex token OR Jellyfin API key
	UserID   string     `mapstructure:"user_id"`   // Jellyfin only
	Username string     `mapstructure:"username"`  // Jellyfin only (display)
	DeviceID string     `mapstructure:"device_id"` // Unique per-install device identifier
}

// PlayerConfig holds media player configuration
type PlayerConfig struct {
	Command   string   `mapstructure:"command"`
	Args      []string `mapstructure:"args"`
	StartFlag string   `mapstructure:"start_flag"` // e.g., "--start=%d" or "--start-time=%d"
}

// UIConfig holds UI configuration
type UIConfig struct {
	ShowWatchStatus   bool `mapstructure:"show_watch_status"`   // Show watched/unwatched/in-progress indicators
	ShowLibraryCounts bool `mapstructure:"show_library_counts"` // Show known library item counts, independently of loading activity
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	File  string `mapstructure:"file"`
	Level string `mapstructure:"level"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		v: viper.New(),
		UI: UIConfig{
			ShowWatchStatus:   true,
			ShowLibraryCounts: false,
		},
		Logging: LoggingConfig{
			File:  defaultLogPath(),
			Level: "INFO",
		},
	}
}

// defaultLogPath returns the default log file path for the current OS
func defaultLogPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "kino", "kino.log")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "kino", "kino.log")
	}
}

// defaultConfigPath returns the default config file path for the current OS
func defaultConfigPath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("APPDATA"), "kino")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".config", "kino")
	}
}

// Load reads the config file from the default directory or the working
// directory, applies KINO_* environment overrides, and remembers the file so
// Save writes back to the same place.
func Load() (*Config, error) {
	cfg := DefaultConfig()
	v := viper.New()
	cfg.v = v

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(defaultConfigPath())
	v.AddConfigPath(".")
	// The config file contains the server token: never world-readable
	v.SetConfigPermissions(0o600)

	// Environment variable overrides. Both pieces are required for nested
	// keys to actually work: the replacer maps server.token →
	// KINO_SERVER_TOKEN, and explicit BindEnv registers each key so
	// Unmarshal sees env-only values (AutomaticEnv alone is invisible to
	// Unmarshal for keys absent from defaults/config).
	v.SetEnvPrefix("KINO")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()
	for _, key := range []string{
		"server.type", "server.url", "server.token", "server.user_id",
		"server.username", "server.device_id",
		"player.command", "player.start_flag",
		"ui.show_watch_status", "ui.show_library_counts",
		"logging.file", "logging.level",
	} {
		_ = v.BindEnv(key)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is OK, use defaults
	}

	// Credentials require private permissions even when the file exists.
	if configFile := v.ConfigFileUsed(); configFile != "" {
		_ = os.Chmod(configFile, 0o600)
	}

	if err := v.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	// Ensure this install has a stable, unique device ID. Media servers
	// (Jellyfin in particular) revoke tokens when another login reuses the
	// same device ID, so a shared/static ID causes intermittent auth failures.
	if cfg.Server.DeviceID == "" {
		cfg.Server.DeviceID = generateDeviceID()
		// Persist immediately for already-configured installs so the ID
		// stays stable across runs. Fresh installs save during setup.
		if v.ConfigFileUsed() != "" {
			if err := cfg.Save(); err != nil {
				return nil, fmt.Errorf("failed to save device ID: %w", err)
			}
		}
	}

	return cfg, nil
}

// generateDeviceID returns a random unique identifier for this install
func generateDeviceID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		// Entropy failure permits authentication with a shared device ID.
		return "kino-tui-client"
	}
	return "kino-" + hex.EncodeToString(buf)
}

// Save writes the configuration back to the file it was loaded from (a
// ./config.yaml stays in place instead of forking a stale copy into the
// default path), or to the default path for fresh installs.
func (c *Config) Save() error {
	file := c.v.ConfigFileUsed()
	if file == "" {
		dir := defaultConfigPath()
		if err := os.MkdirAll(dir, 0755); err != nil {
			return fmt.Errorf("failed to create config directory: %w", err)
		}
		file = filepath.Join(dir, "config.yaml")
	}

	for key, value := range map[string]any{
		"server.type":            c.Server.Type,
		"server.url":             c.Server.URL,
		"server.token":           c.Server.Token,
		"server.user_id":         c.Server.UserID,
		"server.username":        c.Server.Username,
		"server.device_id":       c.Server.DeviceID,
		"player.command":         c.Player.Command,
		"player.args":            c.Player.Args,
		"player.start_flag":      c.Player.StartFlag,
		"ui.show_watch_status":   c.UI.ShowWatchStatus,
		"ui.show_library_counts": c.UI.ShowLibraryCounts,
		"logging.file":           c.Logging.File,
		"logging.level":          c.Logging.Level,
	} {
		c.v.Set(key, value)
	}
	if err := c.v.WriteConfigAs(file); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	return nil
}

// ClearServer removes the server and credentials, keeping the device ID and
// all other settings, and saves the result.
func (c *Config) ClearServer() error {
	c.Server = ServerConfig{DeviceID: c.Server.DeviceID}
	return c.Save()
}

// IsConfigured returns true if the server URL and token are set
func (c *Config) IsConfigured() bool {
	return c.Server.URL != "" && c.Server.Token != ""
}

// DefaultCachePath returns the default cache directory path for the current OS
func DefaultCachePath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "kino", "cache")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "kino", "cache")
	}
}

// CacheDir returns the cache directory for one server and user. Watch state,
// resume positions and playlists are per user, so two accounts on the same
// server never share a cache. (Plex configs have no user ID; those stay keyed
// by URL alone.)
func CacheDir(serverURL, userID string) string {
	normalized := strings.TrimRight(strings.ToLower(serverURL+"|"+userID), "/")
	hash := sha256.Sum256([]byte(normalized))
	return filepath.Join(DefaultCachePath(), hex.EncodeToString(hash[:6]))
}

// ClearCache removes all cached data
func ClearCache() error {
	cachePath := DefaultCachePath()
	if err := os.RemoveAll(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear cache: %w", err)
	}
	return nil
}
