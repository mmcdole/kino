package playlist

import "github.com/mmcdole/kino/internal/domain"

// Queries provides synchronous, cache-only reads.
// Implements domain.PlaylistQueries.
type Queries struct {
	store domain.Store
}

// NewQueries creates a new Queries instance.
func NewQueries(store domain.Store) *Queries {
	return &Queries{store: store}
}

func (q *Queries) GetCachedPlaylists() ([]*domain.Playlist, bool) {
	return q.store.GetPlaylists()
}

func (q *Queries) GetCachedPlaylistItems(playlistID string) ([]*domain.MediaItem, bool) {
	return q.store.GetPlaylistItems(playlistID)
}
