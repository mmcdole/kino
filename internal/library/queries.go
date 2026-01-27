package library

import "github.com/mmcdole/kino/internal/domain"

// Queries provides synchronous, cache-only reads.
// Implements domain.LibraryQueries.
type Queries struct {
	store domain.Store
}

// NewQueries creates a new Queries instance.
func NewQueries(store domain.Store) *Queries {
	return &Queries{store: store}
}

func (q *Queries) GetCachedLibraries() ([]domain.Library, bool) {
	return q.store.GetLibraries()
}

func (q *Queries) GetCachedMovies(libID string) ([]*domain.MediaItem, bool) {
	return q.store.GetMovies(libID)
}

func (q *Queries) GetCachedShows(libID string) ([]*domain.Show, bool) {
	return q.store.GetShows(libID)
}

func (q *Queries) GetCachedMixedContent(libID string) ([]domain.ListItem, bool) {
	return q.store.GetMixedContent(libID)
}

func (q *Queries) GetCachedSeasons(libID, showID string) ([]*domain.Season, bool) {
	return q.store.GetSeasons(libID, showID)
}

func (q *Queries) GetCachedEpisodes(libID, showID, seasonID string) ([]*domain.MediaItem, bool) {
	return q.store.GetEpisodes(libID, showID, seasonID)
}
