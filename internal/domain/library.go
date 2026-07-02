package domain

import "context"

// LibraryClient provides network operations for library browsing.
type LibraryClient interface {
	GetLibraries(ctx context.Context) ([]Library, error)
	GetMovies(ctx context.Context, libID string, offset, limit int) ([]*MediaItem, int, error)
	GetShows(ctx context.Context, libID string, offset, limit int) ([]*Show, int, error)
	GetMixedContent(ctx context.Context, libID string, offset, limit int) ([]ListItem, int, error)
	GetSeasons(ctx context.Context, showID string) ([]*Season, error)
	GetEpisodes(ctx context.Context, seasonID string) ([]*MediaItem, error)

	// GetLibraryItemCount returns the number of top-level items in a library
	// (movies and/or shows, matching what a full sync would fetch) without
	// downloading them. Used for cheap cache validation: library timestamps
	// don't reliably change when items are added.
	GetLibraryItemCount(ctx context.Context, libID, libType string) (int, error)
}
