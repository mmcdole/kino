package adapter

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"github.com/spf13/viper"
)

// Config holds all application configuration
type Config struct {
	Server  ServerConfig  `mapstructure:"server"`
	Player  PlayerConfig  `mapstructure:"player"`
	UI      UIConfig      `mapstructure:"ui"`
	Logging LoggingConfig `mapstructure:"logging"`
}

// ServerConfig holds Plex server configuration
type ServerConfig struct {
	URL   string `mapstructure:"url"`
	Token string `mapstructure:"token"`
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
			URL:   "",
			Token: "",
		},
		Player: PlayerConfig{
			Command: "mpv",
			Args:    []string{},
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

	viper.Set("server", cfg.Server)
	viper.Set("player", cfg.Player)
	viper.Set("ui", cfg.UI)
	viper.Set("logging", cfg.Logging)

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
