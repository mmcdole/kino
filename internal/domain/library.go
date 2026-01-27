package domain

import "context"

// LibraryQueries: Synchronous, cache-only reads.
// All methods return instantly. NEVER block on network.
// Safe to call from View() and navigation code.
type LibraryQueries interface {
	GetCachedLibraries() ([]Library, bool)
	GetCachedMovies(libID string) ([]*MediaItem, bool)
	GetCachedShows(libID string) ([]*Show, bool)
	GetCachedMixedContent(libID string) ([]ListItem, bool)
	GetCachedSeasons(libID, showID string) ([]*Season, bool)
	GetCachedEpisodes(libID, showID, seasonID string) ([]*MediaItem, bool)
}

// LibraryCommands: Asynchronous operations that may hit network.
// Must be called from tea.Cmd functions, never from View().
type LibraryCommands interface {
	// Always fetch (to get current UpdatedAt timestamps)
	FetchLibraries(ctx context.Context) ([]Library, error)

	// Smart sync (Startup): Check timestamp, fetch if stale
	SyncLibrary(ctx context.Context, lib Library, onProgress ProgressFunc) (SyncResult, error)

	// Force fetch (Manual 'r' or cache miss): Always download
	FetchMovies(ctx context.Context, libID string, onProgress ProgressFunc) ([]*MediaItem, error)
	FetchShows(ctx context.Context, libID string, onProgress ProgressFunc) ([]*Show, error)
	FetchMixedContent(ctx context.Context, libID string, onProgress ProgressFunc) ([]ListItem, error)
	FetchSeasons(ctx context.Context, libID, showID string) ([]*Season, error)
	FetchEpisodes(ctx context.Context, libID, showID, seasonID string) ([]*MediaItem, error)

	// Cache invalidation
	InvalidateLibrary(libID string)
	InvalidateShow(libID, showID string)
	InvalidateSeason(libID, showID, seasonID string)
	InvalidateAll()
}

// LibraryRepository: Network operations (implemented by mediaserver clients)
type LibraryRepository interface {
	GetLibraries(ctx context.Context) ([]Library, error)
	GetMovies(ctx context.Context, libID string, offset, limit int) ([]*MediaItem, int, error)
	GetShows(ctx context.Context, libID string, offset, limit int) ([]*Show, int, error)
	GetLibraryContent(ctx context.Context, libID string, offset, limit int) ([]ListItem, int, error)
	GetSeasons(ctx context.Context, showID string) ([]*Season, error)
	GetEpisodes(ctx context.Context, seasonID string) ([]*MediaItem, error)
}

// SearchRepository provides search functionality across libraries
type SearchRepository interface {
	Search(ctx context.Context, query string) ([]*MediaItem, error)
}

// MetadataRepository provides detailed metadata and URL resolution
type MetadataRepository interface {
	ResolvePlayableURL(ctx context.Context, itemID string) (string, error)
	GetMediaItem(ctx context.Context, itemID string) (*MediaItem, error)
	MarkPlayed(ctx context.Context, itemID string) error
	MarkUnplayed(ctx context.Context, itemID string) error
}
