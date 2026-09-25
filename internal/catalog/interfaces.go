package catalog

import (
	"context"

	"github.com/mmcdole/kino/internal/domain"
)

// Backend and Cache describe the operations consumed by this application
// service. Concrete adapters remain independently testable.
type Backend interface {
	GetLibraries(ctx context.Context) ([]domain.Library, error)
	GetMovies(ctx context.Context, libID string, offset, limit int) ([]*domain.MediaItem, int, error)
	GetShows(ctx context.Context, libID string, offset, limit int) ([]*domain.Show, int, error)
	GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]domain.ListItem, int, error)
	GetSeasons(ctx context.Context, showID string) ([]*domain.Season, error)
	GetEpisodes(ctx context.Context, seasonID string) ([]*domain.MediaItem, error)

	// GetLibraryItemCount returns the number of top-level items in a library
	// (movies and/or shows, matching what a full sync would fetch) without
	// downloading them. Used for cheap cache validation: library timestamps
	// don't reliably change when items are added.
	GetLibraryItemCount(ctx context.Context, libID, libType string) (int, error)

	GetPlaylists(ctx context.Context) ([]*domain.Playlist, error)
	GetPlaylistItems(ctx context.Context, playlistID string) ([]*domain.MediaItem, error)
	CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*domain.Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error
	RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error
	DeletePlaylist(ctx context.Context, playlistID string) error

	MarkPlayed(ctx context.Context, itemID string) error
	MarkUnplayed(ctx context.Context, itemID string) error
}

// Cache supports concurrent reads and returns detached entity values. Update
// changes every snapshot mentioning one of the IDs in a single transaction.
type Cache interface {
	Load(string) (domain.CachedList, bool)
	Save(string, domain.CachedList) error
	Update(ids []string, fn func(map[string]domain.CachedList) map[string]domain.CachedList) ([]string, error)
}
