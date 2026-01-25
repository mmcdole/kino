package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

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
	Server      ServerConfig      `mapstructure:"server"`
	Player      PlayerConfig      `mapstructure:"player"`
	Preferences PreferencesConfig `mapstructure:"preferences"`
	UI          UIConfig          `mapstructure:"ui"`
	Logging     LoggingConfig     `mapstructure:"logging"`
}

// ServerConfig holds media server configuration
type ServerConfig struct {
	Type     SourceType `mapstructure:"type"`     // "plex" or "jellyfin"
	URL      string     `mapstructure:"url"`      // Server URL
	Token    string     `mapstructure:"token"`    // Plex token OR Jellyfin API key
	UserID   string     `mapstructure:"user_id"`  // Jellyfin only
	Username string     `mapstructure:"username"` // Jellyfin only (display)
}

// PreferencesConfig holds user preferences
type PreferencesConfig struct {
	ShowWatchStatus bool `mapstructure:"show_watch_status"` // Show watched/unwatched/in-progress indicators
}

// PlayerConfig holds media player configuration
type PlayerConfig struct {
	Command   string   `mapstructure:"command"`
	Args      []string `mapstructure:"args"`
	StartFlag string   `mapstructure:"start_flag"` // e.g., "--start=" or "--start-time="
}

// UIConfig holds UI configuration
type UIConfig struct {
	Theme       string `mapstructure:"theme"`
	GridColumns int    `mapstructure:"grid_columns"`
	DefaultView string `mapstructure:"default_view"`
}

// LoggingConfig holds logging configuration
type LoggingConfig struct {
	File  string `mapstructure:"file"`
	Level string `mapstructure:"level"`
}

// DefaultConfig returns the default configuration
func DefaultConfig() *Config {
	return &Config{
		Server: ServerConfig{
			Type:  "",
			URL:   "",
			Token: "",
		},
		Player: PlayerConfig{
			Command: "mpv",
			Args:    []string{},
		},
		Preferences: PreferencesConfig{
			ShowWatchStatus: true,
		},
		UI: UIConfig{
			Theme:       "default",
			GridColumns: 4,
			DefaultView: "grid",
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

// LoadConfig loads configuration from file and environment
func LoadConfig() (*Config, error) {
	cfg := DefaultConfig()

	viper.SetConfigName("config")
	viper.SetConfigType("yaml")
	viper.AddConfigPath(defaultConfigPath())
	viper.AddConfigPath(".")

	// Environment variable overrides
	viper.SetEnvPrefix("KINO")
	viper.AutomaticEnv()

	// Read config file if it exists
	if err := viper.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("error reading config file: %w", err)
		}
		// Config file not found is OK, use defaults
	}

	if err := viper.Unmarshal(cfg); err != nil {
		return nil, fmt.Errorf("error parsing config: %w", err)
	}

	return cfg, nil
}

// SaveConfig saves the current configuration to file
func SaveConfig(cfg *Config) error {
	configPath := defaultConfigPath()

	// Ensure config directory exists
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// Set server fields individually to ensure correct key names (snake_case)
	viper.Set("server.type", cfg.Server.Type)
	viper.Set("server.url", cfg.Server.URL)
	viper.Set("server.token", cfg.Server.Token)
	viper.Set("server.user_id", cfg.Server.UserID)
	viper.Set("server.username", cfg.Server.Username)

	// Set player fields
	viper.Set("player.command", cfg.Player.Command)
	viper.Set("player.args", cfg.Player.Args)
	viper.Set("player.start_flag", cfg.Player.StartFlag)

	// Set preferences
	viper.Set("preferences.show_watch_status", cfg.Preferences.ShowWatchStatus)

	// Set UI fields
	viper.Set("ui.theme", cfg.UI.Theme)
	viper.Set("ui.grid_columns", cfg.UI.GridColumns)
	viper.Set("ui.default_view", cfg.UI.DefaultView)

	// Set logging fields
	viper.Set("logging.file", cfg.Logging.File)
	viper.Set("logging.level", cfg.Logging.Level)

	configFile := filepath.Join(configPath, "config.yaml")
	if err := viper.WriteConfigAs(configFile); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// SaveToken updates just the token in the configuration
func SaveToken(token string) error {
	viper.Set("server.token", token)

	configPath := defaultConfigPath()
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configFile := filepath.Join(configPath, "config.yaml")
	if err := viper.WriteConfigAs(configFile); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// IsConfigured returns true if the server URL and token are set
func (c *Config) IsConfigured() bool {
	return c.Server.URL != "" && c.Server.Token != ""
}

// defaultCachePath returns the default cache directory path for the current OS
func defaultCachePath() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "kino", "cache")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "kino", "cache")
	}
}

// ClearServerConfig removes all server-related configuration (type, URL, credentials)
// while preserving other settings (player, UI, logging, preferences)
func ClearServerConfig() error {
	// Clear server fields in viper
	viper.Set("server.type", "")
	viper.Set("server.url", "")
	viper.Set("server.token", "")
	viper.Set("server.user_id", "")
	viper.Set("server.username", "")

	configPath := defaultConfigPath()
	if err := os.MkdirAll(configPath, 0755); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	configFile := filepath.Join(configPath, "config.yaml")
	if err := viper.WriteConfigAs(configFile); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// ClearCache removes all cached data
func ClearCache() error {
	cachePath := defaultCachePath()
	if err := os.RemoveAll(cachePath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear cache: %w", err)
	}
	return nil
}

// GetCachePath returns the cache directory path
func GetCachePath() string {
	return defaultCachePath()
}
