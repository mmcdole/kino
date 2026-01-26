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

	// GetLibraryContent returns paginated content (movies AND shows) from a mixed library
	// Returns (items, totalSize, error) for pagination support
	GetLibraryContent(ctx context.Context, libID string, offset, limit int) ([]ListItem, int, error)

	// GetAllMovies returns all movies in a library (handles pagination internally)
	GetAllMovies(ctx context.Context, libID string) ([]*MediaItem, error)

	// GetAllShows returns all TV shows in a library (handles pagination internally)
	GetAllShows(ctx context.Context, libID string) ([]*Show, error)

	// GetAllLibraryContent returns all content from a mixed library (handles pagination internally)
	GetAllLibraryContent(ctx context.Context, libID string) ([]ListItem, error)

	// GetMoviesWithProgress fetches movies and reports progress via callback
	// The callback receives each batch as it's fetched: (batch, loadedSoFar, total)
	GetMoviesWithProgress(ctx context.Context, libID string, progress func([]*MediaItem, int, int)) error

	// GetShowsWithProgress fetches shows and reports progress via callback
	GetShowsWithProgress(ctx context.Context, libID string, progress func([]*Show, int, int)) error

	// GetLibraryContentWithProgress fetches mixed library content and reports progress via callback
	GetLibraryContentWithProgress(ctx context.Context, libID string, progress func([]ListItem, int, int)) error

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

// PlaylistRepository provides access to playlist management operations
type PlaylistRepository interface {
	// GetPlaylists returns all user playlists
	GetPlaylists(ctx context.Context) ([]*Playlist, error)

	// GetPlaylistItems returns all items in a playlist
	GetPlaylistItems(ctx context.Context, playlistID string) ([]*MediaItem, error)

	// CreatePlaylist creates a new playlist with the given title and optional initial items
	CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*Playlist, error)

	// AddToPlaylist adds items to an existing playlist
	AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error

	// RemoveFromPlaylist removes an item from a playlist
	RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error

	// DeletePlaylist deletes a playlist
	DeletePlaylist(ctx context.Context, playlistID string) error
}

// AuthProvider handles authentication with the media server (legacy Plex-specific interface)
type AuthProvider interface {
	// GetPIN generates a new authentication PIN
	GetPIN(ctx context.Context) (pin string, id int, err error)

	// CheckPIN polls for PIN claim status and returns the auth token
	CheckPIN(ctx context.Context, pinID int) (token string, claimed bool, err error)

	// ValidateToken checks if a token is still valid
	ValidateToken(ctx context.Context, token string) error
}

// AuthResult contains the result of a successful authentication
type AuthResult struct {
	Token    string // Access token for API calls
	UserID   string // User identifier (required for Jellyfin)
	Username string // Display username
}

// AuthFlow defines a generic authentication flow for any media server.
// Different backends implement this differently:
// - Plex: PIN-based OAuth flow (display PIN -> user visits plex.tv/link -> poll for token)
// - Jellyfin: Username/password authentication
type AuthFlow interface {
	// Run executes the authentication flow and returns credentials.
	// The serverURL parameter is the base URL of the media server.
	// Implementations handle their own user interaction (prompting for credentials, etc.)
	Run(ctx context.Context, serverURL string) (*AuthResult, error)
}
