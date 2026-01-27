package domain

import "context"

// PlaylistQueries: Synchronous, cache-only reads.
type PlaylistQueries interface {
	GetCachedPlaylists() ([]*Playlist, bool)
	GetCachedPlaylistItems(playlistID string) ([]*MediaItem, bool)
}

// PlaylistCommands: Asynchronous operations (includes CRUD).
type PlaylistCommands interface {
	// Force fetch
	FetchPlaylists(ctx context.Context) ([]*Playlist, error)
	FetchPlaylistItems(ctx context.Context, playlistID string) ([]*MediaItem, error)

	// Sync playlists and their items (two levels deep, like library sync)
	SyncPlaylists(ctx context.Context) ([]*Playlist, error)

	// CRUD (each invalidates cache after success)
	CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error
	RemoveFromPlaylist(ctx context.Context, playlistID, itemID string) error
	DeletePlaylist(ctx context.Context, playlistID string) error

	// Playlist membership check (uses cache)
	GetPlaylistMembership(ctx context.Context, itemID string) (map[string]bool, error)

	// Cache invalidation
	InvalidatePlaylists()
	InvalidatePlaylistItems(playlistID string)
}

// PlaylistRepository: Network operations (implemented by mediaserver clients)
type PlaylistRepository interface {
	GetPlaylists(ctx context.Context) ([]*Playlist, error)
	GetPlaylistItems(ctx context.Context, playlistID string) ([]*MediaItem, error)
	CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error
	RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error
	DeletePlaylist(ctx context.Context, playlistID string) error
}
