package source

import (
	"fmt"
	"log/slog"

	"github.com/mmcdole/kino/internal/adapter"
	"github.com/mmcdole/kino/internal/adapter/source/jellyfin"
	"github.com/mmcdole/kino/internal/adapter/source/plex"
	"github.com/mmcdole/kino/internal/domain"
)

// MediaSource combines all repository interfaces that a media server backend must implement.
// This is the unified interface for browsing, metadata, search, and playlist operations.
type MediaSource interface {
	domain.LibraryRepository   // Browsing: GetLibraries, GetMovies, GetShows, GetSeasons, GetEpisodes
	domain.MetadataRepository  // Playback: ResolvePlayableURL, MarkPlayed/Unplayed, GetNextEpisode
	domain.SearchRepository    // Search: Search(query) across all libraries
	domain.PlaylistRepository  // Playlists: GetPlaylists, CreatePlaylist, AddToPlaylist, etc.
}

// SourceConfig contains the configuration needed to create a MediaSource
type SourceConfig struct {
	Type     adapter.SourceType
	URL      string
	Token    string
	UserID   string // Jellyfin only
	Username string // Jellyfin only (for display)
}

// NewClient creates a new MediaSource based on the server type.
// This factory function abstracts away the specific backend implementation.
func NewClient(cfg *SourceConfig, logger *slog.Logger) (MediaSource, error) {
	if cfg == nil {
		return nil, fmt.Errorf("source config is nil")
	}

	if cfg.URL == "" {
		return nil, fmt.Errorf("server URL is required")
	}

	if cfg.Token == "" {
		return nil, fmt.Errorf("server token is required")
	}

	switch cfg.Type {
	case adapter.SourceTypePlex:
		return plex.NewClient(cfg.URL, cfg.Token, logger), nil

	case adapter.SourceTypeJellyfin:
		if cfg.UserID == "" {
			return nil, fmt.Errorf("Jellyfin requires user ID")
		}
		return jellyfin.NewClient(cfg.URL, cfg.Token, cfg.UserID, logger), nil

	default:
		return nil, fmt.Errorf("unknown server type: %s", cfg.Type)
	}
}

// NewClientFromConfig creates a MediaSource from the application config
func NewClientFromConfig(cfg *adapter.Config, logger *slog.Logger) (MediaSource, error) {
	return NewClient(&SourceConfig{
		Type:     cfg.Server.Type,
		URL:      cfg.Server.URL,
		Token:    cfg.Server.Token,
		UserID:   cfg.Server.UserID,
		Username: cfg.Server.Username,
	}, logger)
}
