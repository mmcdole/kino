package mediaserver

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/mmcdole/kino/internal/config"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/mediaserver/jellyfin"
	"github.com/mmcdole/kino/internal/mediaserver/plex"
)

// MediaSource combines all repository interfaces that a media server backend must implement.
// This is the unified interface for browsing, metadata, search, and playlist operations.
type MediaSource interface {
	domain.LibraryRepository  // Browsing: GetLibraries, GetMovies, GetShows, GetSeasons, GetEpisodes
	domain.MetadataRepository // Playback: ResolvePlayableURL, MarkPlayed/Unplayed
	domain.SearchRepository   // Search: Search(query) across all libraries
	domain.PlaylistRepository // Playlists: GetPlaylists, CreatePlaylist, AddToPlaylist, etc.
}

// NewClient creates a new MediaSource based on the server type.
// This factory function abstracts away the specific backend implementation.
func NewClient(cfg *config.Config, logger *slog.Logger) (MediaSource, error) {
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
	case config.SourceTypePlex:
		client := plex.NewClient(cfg.Server.URL, cfg.Server.Token, logger)

		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := client.FetchIdentity(ctx); err != nil {
			logger.Warn("failed to fetch plex identity", "error", err)
			// Non-fatal: playlist creation will fail but browsing works
		}

		return client, nil

	case config.SourceTypeJellyfin:
		if cfg.Server.UserID == "" {
			return nil, fmt.Errorf("Jellyfin requires user ID")
		}
		return jellyfin.NewClient(cfg.Server.URL, cfg.Server.Token, cfg.Server.UserID, logger), nil

	default:
		return nil, fmt.Errorf("unknown server type: %s", cfg.Server.Type)
	}
}
