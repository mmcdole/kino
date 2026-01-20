package domain

import (
	"context"
)

// LibraryRepository provides access to media libraries and their content
type LibraryRepository interface {
	// GetLibraries returns all available libraries
	GetLibraries(ctx context.Context) ([]Library, error)

	// GetLibraryDetails returns details for a specific library (lightweight)
	GetLibraryDetails(ctx context.Context, libID string) (*Library, error)

	// GetMovies returns paginated movies from a movie library
	// Returns (items, totalSize, error) for pagination support
	GetMovies(ctx context.Context, libID string, offset, limit int) ([]*MediaItem, int, error)

	// GetShows returns paginated TV shows from a show library
	// Returns (items, totalSize, error) for pagination support
	GetShows(ctx context.Context, libID string, offset, limit int) ([]*Show, int, error)

	// GetAllMovies returns all movies in a library (handles pagination internally)
	GetAllMovies(ctx context.Context, libID string) ([]*MediaItem, error)

	// GetAllShows returns all TV shows in a library (handles pagination internally)
	GetAllShows(ctx context.Context, libID string) ([]*Show, error)

	// GetMoviesWithProgress fetches movies and reports progress via callback
	// The callback receives each batch as it's fetched: (batch, loadedSoFar, total)
	GetMoviesWithProgress(ctx context.Context, libID string, progress func([]*MediaItem, int, int)) error

	// GetShowsWithProgress fetches shows and reports progress via callback
	GetShowsWithProgress(ctx context.Context, libID string, progress func([]*Show, int, int)) error

	// GetSeasons returns all seasons for a TV show
	GetSeasons(ctx context.Context, showID string) ([]*Season, error)

	// GetEpisodes returns all episodes for a season
	GetEpisodes(ctx context.Context, seasonID string) ([]*MediaItem, error)

	// GetRecentlyAdded returns recently added items from a library
	GetRecentlyAdded(ctx context.Context, libID string, limit int) ([]*MediaItem, error)
}

// SearchRepository provides search functionality across libraries
type SearchRepository interface {
	// Search performs a fuzzy search across all libraries
	Search(ctx context.Context, query string) ([]MediaItem, error)
}

// MetadataRepository provides detailed metadata and URL resolution
type MetadataRepository interface {
	// ResolvePlayableURL returns a direct playback URL for an item
	ResolvePlayableURL(ctx context.Context, itemID string) (string, error)

	// GetNextEpisode returns the next episode in a series
	GetNextEpisode(ctx context.Context, episodeID string) (*MediaItem, error)

	// GetMediaItem returns detailed metadata for a specific item
	GetMediaItem(ctx context.Context, itemID string) (*MediaItem, error)

	// MarkPlayed marks an item as fully watched
	MarkPlayed(ctx context.Context, itemID string) error

	// MarkUnplayed marks an item as unwatched
	MarkUnplayed(ctx context.Context, itemID string) error
}

// AuthProvider handles authentication with the media server
type AuthProvider interface {
	// GetPIN generates a new authentication PIN
	GetPIN(ctx context.Context) (pin string, id int, err error)

	// CheckPIN polls for PIN claim status and returns the auth token
	CheckPIN(ctx context.Context, pinID int) (token string, claimed bool, err error)

	// ValidateToken checks if a token is still valid
	ValidateToken(ctx context.Context, token string) error
}
