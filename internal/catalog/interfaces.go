package catalog

import (
	"context"

	"github.com/mmcdole/kino/internal/media"
)

// Backend and Cache describe the operations consumed by this application
// service. Concrete adapters remain independently testable.
type Backend interface {
	GetLibraries(ctx context.Context) ([]media.Library, error)
	GetMovies(ctx context.Context, libID string, offset, limit int) ([]*media.Item, int, error)
	GetShows(ctx context.Context, libID string, offset, limit int) ([]*media.Show, int, error)
	GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]media.ListItem, int, error)
	GetSeasons(ctx context.Context, showID string) ([]*media.Season, error)
	GetEpisodes(ctx context.Context, seasonID string) ([]*media.Item, error)

	// GetLibraryItemCount returns the number of top-level items in a library
	// (movies and/or shows, matching what a full sync would fetch) without
	// downloading them. Used for cheap cache validation: library timestamps
	// don't reliably change when items are added.
	GetLibraryItemCount(ctx context.Context, libID, libType string) (int, error)

	GetPlaylists(ctx context.Context) ([]*media.Playlist, error)
	GetPlaylistItems(ctx context.Context, playlistID string) ([]*media.Item, error)
	CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*media.Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error
	RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error
	DeletePlaylist(ctx context.Context, playlistID string) error

	MarkPlayed(ctx context.Context, itemID string) error
	MarkUnplayed(ctx context.Context, itemID string) error
}

// Cache supports concurrent reads and returns detached entity values. Update
// changes every snapshot mentioning one of the IDs in a single transaction.
type Cache interface {
	Load(string) (media.CachedList, bool)
	Save(string, media.CachedList) error
	Update(ids []string, fn func(map[string]media.CachedList) map[string]media.CachedList) ([]string, error)
}
