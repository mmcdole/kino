package domain

// Store handles local cache (BoltDB + memory).
// TUI reads directly from Store for cache access.
type Store interface {
	// === Libraries ===
	GetLibraries() ([]Library, bool)
	SaveLibraries(libs []Library) error

	// === Library Content ===
	GetMovies(libID string) ([]*MediaItem, bool)
	SaveMovies(libID string, movies []*MediaItem, serverTS int64) error

	GetShows(libID string) ([]*Show, bool)
	SaveShows(libID string, shows []*Show, serverTS int64) error

	GetMixedContent(libID string) ([]ListItem, bool)
	SaveMixedContent(libID string, items []ListItem, serverTS int64) error

	// === Hierarchical (TV) ===
	GetSeasons(libID, showID string) ([]*Season, bool)
	SaveSeasons(libID, showID string, seasons []*Season) error

	GetEpisodes(libID, showID, seasonID string) ([]*MediaItem, bool)
	SaveEpisodes(libID, showID, seasonID string, episodes []*MediaItem) error

	// === Playlists ===
	GetPlaylists() ([]*Playlist, bool)
	SavePlaylists(playlists []*Playlist) error

	GetPlaylistItems(playlistID string) ([]*MediaItem, bool)
	SavePlaylistItems(playlistID string, items []*MediaItem) error

	// === Freshness ===
	IsValid(libID string, serverTS int64) bool

	// === Invalidation ===
	InvalidateLibrary(libID string)
	InvalidateShow(libID, showID string)
	InvalidateSeason(libID, showID, seasonID string)
	InvalidatePlaylists()
	InvalidatePlaylistItems(playlistID string)
	InvalidateAll()

	Close() error
}
