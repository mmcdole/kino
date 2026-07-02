package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// KINO_* environment overrides must reach the unmarshaled config — they were
// previously dead (no env key replacer, no BindEnv registration).
func TestEnvVarOverrides(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home)
	t.Setenv("KINO_SERVER_TOKEN", "env-token")
	t.Setenv("KINO_LOGGING_LEVEL", "DEBUG")

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Server.Token != "env-token" {
		t.Fatalf("KINO_SERVER_TOKEN ignored: token = %q", cfg.Server.Token)
	}
	if cfg.Logging.Level != "DEBUG" {
		t.Fatalf("KINO_LOGGING_LEVEL ignored: level = %q", cfg.Logging.Level)
	}
}

// TestLoadConfigGeneratesDeviceID verifies that an already-configured install
// without a device_id (pre-existing config.yaml) gets a unique ID generated
// and persisted, so the ID stays stable across runs.
func TestLoadConfigGeneratesDeviceID(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("APPDATA", home) // windows

	configDir := filepath.Join(home, ".config", "kino")
	if err := os.MkdirAll(configDir, 0755); err != nil {
		t.Fatal(err)
	}

	existing := `server:
  type: "jellyfin"
  url: "http://localhost:8096"
  token: "abc123"
  user_id: "user1"
`
	configFile := filepath.Join(configDir, "config.yaml")
	if err := os.WriteFile(configFile, []byte(existing), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig: %v", err)
	}

	if cfg.Server.DeviceID == "" {
		t.Fatal("expected device ID to be generated")
	}
	if cfg.Server.DeviceID == "kino-tui-client" {
		t.Fatal("device ID should be unique, not the legacy shared ID")
	}
	if !strings.HasPrefix(cfg.Server.DeviceID, "kino-") {
		t.Fatalf("unexpected device ID format: %q", cfg.Server.DeviceID)
	}

	// Must be persisted so the same ID is used on the next run
	data, err := os.ReadFile(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), cfg.Server.DeviceID) {
		t.Fatalf("device ID %q not persisted to config file:\n%s", cfg.Server.DeviceID, data)
	}

	// Existing credentials must survive the rewrite
	if !strings.Contains(string(data), "abc123") {
		t.Fatalf("token lost when persisting device ID:\n%s", data)
	}

	// The config file contains the token: it must not be world-readable,
	// including pre-existing files created with the old 0644 default
	info, err := os.Stat(configFile)
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("config file mode = %o, want 600", perm)
	}
}
