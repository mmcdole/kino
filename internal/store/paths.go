package store

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// baseDir returns the root cache directory for the current OS.
func baseDir() string {
	switch runtime.GOOS {
	case "windows":
		return filepath.Join(os.Getenv("LOCALAPPDATA"), "kino", "cache")
	default:
		home, _ := os.UserHomeDir()
		return filepath.Join(home, ".local", "share", "kino", "cache")
	}
}

// Dir returns the cache directory for one server and user. Watch state,
// resume positions and playlists are per user, so two accounts on the same
// server never share a cache. (Plex configs have no user ID; those stay keyed
// by URL alone.)
func Dir(serverURL, userID string) string {
	normalized := strings.TrimRight(strings.ToLower(serverURL+"|"+userID), "/")
	hash := sha256.Sum256([]byte(normalized))
	return filepath.Join(baseDir(), hex.EncodeToString(hash[:6]))
}

// Clear removes all cached data for every server.
func Clear() error {
	if err := os.RemoveAll(baseDir()); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to clear cache: %w", err)
	}
	return nil
}
