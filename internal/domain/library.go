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
}
