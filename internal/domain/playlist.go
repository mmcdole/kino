package domain

import "context"

// PlaylistClient provides network operations for playlist management.
type PlaylistClient interface {
	GetPlaylists(ctx context.Context) ([]*Playlist, error)
	GetPlaylistItems(ctx context.Context, playlistID string) ([]*MediaItem, error)
	CreatePlaylist(ctx context.Context, title string, itemIDs []string) (*Playlist, error)
	AddToPlaylist(ctx context.Context, playlistID string, itemIDs []string) error
	RemoveFromPlaylist(ctx context.Context, playlistID string, itemID string) error
	DeletePlaylist(ctx context.Context, playlistID string) error
}
