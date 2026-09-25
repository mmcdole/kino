package mediaserver

import (
	"fmt"
	"log/slog"

	"github.com/mmcdole/kino/internal/catalog"
	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
	"github.com/mmcdole/kino/internal/player"
)

// Server is everything the application needs from a media server: catalog browsing and mutations, and playable URL resolution.
type Server interface {
	catalog.Backend
	player.URLResolver
}

// New returns the client for the configured server type.
func New(cfg *config.Config, logger *slog.Logger) (Server, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is nil")
	}

	if cfg.Server.URL == "" {
		return nil, fmt.Errorf("server URL is required")
	}

	if cfg.Server.Token == "" {
		return nil, fmt.Errorf("server token is required")
	}

	switch cfg.Server.Type {
	case config.Plex:
		return plex.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Server.DeviceID, logger), nil

	case config.Jellyfin:
		if cfg.Server.UserID == "" {
			return nil, fmt.Errorf("Jellyfin requires user ID")
		}
		return jellyfin.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Server.UserID, cfg.Server.DeviceID, logger), nil

	default:
		return nil, fmt.Errorf("unknown server type: %s", cfg.Server.Type)
	}
}
