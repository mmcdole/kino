package domain

// LibraryStore handles hierarchical BoltDB cache.
// Keys encode ancestry (lib:X:show:Y:season:Z) for cascade invalidation via prefix deletion.
type LibraryStore interface {
	// === Libraries ===
	GetLibraries() ([]Library, bool)
	SaveLibraries(libs []Library) error

	// === Top Level (Library Content) ===
	GetMovies(libID string) ([]*MediaItem, bool)
	SaveMovies(libID string, movies []*MediaItem, serverTS int64) error

	GetShows(libID string) ([]*Show, bool)
	SaveShows(libID string, shows []*Show, serverTS int64) error

	GetMixedContent(libID string) ([]ListItem, bool)
	SaveMixedContent(libID string, items []ListItem, serverTS int64) error

	// IsValid checks if stored timestamp >= serverTS
	IsValid(libID string, serverTS int64) bool

	// === Mid Level (Seasons) - Requires libID for cascade invalidation ===
	GetSeasons(libID, showID string) ([]*Season, bool)
	SaveSeasons(libID, showID string, seasons []*Season) error

	// === Leaf Level (Episodes) - Requires full ancestry for cascade ===
	GetEpisodes(libID, showID, seasonID string) ([]*MediaItem, bool)
	SaveEpisodes(libID, showID, seasonID string, episodes []*MediaItem) error

	// === Cascade Invalidation (BoltDB prefix deletion) ===
	InvalidateLibrary(libID string)                  // Wipes content + seasons + episodes
	InvalidateShow(libID, showID string)             // Wipes seasons + episodes for show
	InvalidateSeason(libID, showID, seasonID string) // Wipes episodes for season
	InvalidateAll()                                  // Wipes entire cache

	// === Lifecycle ===
	Close() error
}

// SyncProgress reports progress during library synchronization.
type SyncProgress struct {
	LibraryID   string
	LibraryType string
	Loaded      int
	Total       int
	Done        bool
	FromCache   bool
	Error       error
}

// SyncObserver receives progress updates during sync operations.
type SyncObserver interface {
	OnProgress(progress SyncProgress)
}

// NoOpObserver discards progress updates (for testing/batch operations).
type NoOpObserver struct{}

func (NoOpObserver) OnProgress(SyncProgress) {}
