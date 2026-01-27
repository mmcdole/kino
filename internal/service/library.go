package service

import (
	"context"
	"log/slog"

	"github.com/mmcdole/kino/internal/domain"
)

// LibraryService orchestrates library synchronization.
// Provides fetch-or-cache methods that check Store first, then network on miss.
type LibraryService struct {
	repo   domain.LibraryRepository // Network (Plex/Jellyfin)
	store  domain.LibraryStore      // Local persistence
	logger *slog.Logger
}

// NewLibraryService creates a new library service
func NewLibraryService(
	repo domain.LibraryRepository,
	store domain.LibraryStore,
	logger *slog.Logger,
) *LibraryService {
	if logger == nil {
		logger = slog.Default()
	}
	return &LibraryService{repo: repo, store: store, logger: logger}
}

// GetLibraries fetches library list from network (always fresh).
func (s *LibraryService) GetLibraries(ctx context.Context) ([]domain.Library, error) {
	libs, err := s.repo.GetLibraries(ctx)
	if err != nil {
		s.logger.Error("failed to get libraries", "error", err)
		return nil, err
	}
	s.store.SaveLibraries(libs)
	s.logger.Info("loaded libraries", "count", len(libs))
	return libs, nil
}

// SyncLibrary synchronizes a library's content.
// Checks freshness first; only fetches if stale or forced.
// When force=true, also invalidates cached seasons/episodes (cascade).
func (s *LibraryService) SyncLibrary(
	ctx context.Context,
	lib domain.Library,
	force bool,
	observer domain.SyncObserver,
) {
	// 1. Force refresh: invalidate hierarchical cache (seasons/episodes)
	if force {
		s.store.InvalidateLibrary(lib.ID)
		s.logger.Debug("force refresh: invalidated library cache", "libID", lib.ID)
	}

	// 2. Freshness check (skip if forced since we just invalidated)
	if !force && s.store.IsValid(lib.ID, lib.UpdatedAt) {
		s.reportCacheHit(lib, observer)
		return
	}

	// 3. Fetch from network
	switch lib.Type {
	case "movie":
		s.syncMovies(ctx, lib, observer)
	case "show":
		s.syncShows(ctx, lib, observer)
	default:
		s.syncMixed(ctx, lib, observer)
	}
}

func (s *LibraryService) syncMovies(ctx context.Context, lib domain.Library, observer domain.SyncObserver) {
	movies, err := fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.MediaItem, int, error) {
			return s.repo.GetMovies(ctx, lib.ID, offset, limit)
		},
		defaultChunkSize,
		func(loaded, total int) {
			observer.OnProgress(domain.SyncProgress{
				LibraryID:   lib.ID,
				LibraryType: lib.Type,
				Loaded:      loaded,
				Total:       total,
			})
		},
	)

	if err != nil {
		observer.OnProgress(domain.SyncProgress{
			LibraryID: lib.ID, LibraryType: lib.Type, Error: err, Done: true,
		})
		s.logger.Error("failed to sync movies", "error", err, "libID", lib.ID)
		return
	}

	if err := s.store.SaveMovies(lib.ID, movies, lib.UpdatedAt); err != nil {
		s.logger.Error("failed to save movies", "error", err, "libID", lib.ID)
	}

	observer.OnProgress(domain.SyncProgress{
		LibraryID: lib.ID, LibraryType: lib.Type,
		Loaded: len(movies), Total: len(movies), Done: true,
	})
	s.logger.Info("synced movies", "count", len(movies), "libID", lib.ID)
}

func (s *LibraryService) syncShows(ctx context.Context, lib domain.Library, observer domain.SyncObserver) {
	shows, err := fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.Show, int, error) {
			return s.repo.GetShows(ctx, lib.ID, offset, limit)
		},
		defaultChunkSize,
		func(loaded, total int) {
			observer.OnProgress(domain.SyncProgress{
				LibraryID:   lib.ID,
				LibraryType: lib.Type,
				Loaded:      loaded,
				Total:       total,
			})
		},
	)

	if err != nil {
		observer.OnProgress(domain.SyncProgress{
			LibraryID: lib.ID, LibraryType: lib.Type, Error: err, Done: true,
		})
		s.logger.Error("failed to sync shows", "error", err, "libID", lib.ID)
		return
	}

	if err := s.store.SaveShows(lib.ID, shows, lib.UpdatedAt); err != nil {
		s.logger.Error("failed to save shows", "error", err, "libID", lib.ID)
	}

	observer.OnProgress(domain.SyncProgress{
		LibraryID: lib.ID, LibraryType: lib.Type,
		Loaded: len(shows), Total: len(shows), Done: true,
	})
	s.logger.Info("synced shows", "count", len(shows), "libID", lib.ID)
}

func (s *LibraryService) syncMixed(ctx context.Context, lib domain.Library, observer domain.SyncObserver) {
	items, err := fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]domain.ListItem, int, error) {
			return s.repo.GetLibraryContent(ctx, lib.ID, offset, limit)
		},
		defaultChunkSize,
		func(loaded, total int) {
			observer.OnProgress(domain.SyncProgress{
				LibraryID:   lib.ID,
				LibraryType: lib.Type,
				Loaded:      loaded,
				Total:       total,
			})
		},
	)

	if err != nil {
		observer.OnProgress(domain.SyncProgress{
			LibraryID: lib.ID, LibraryType: lib.Type, Error: err, Done: true,
		})
		s.logger.Error("failed to sync mixed content", "error", err, "libID", lib.ID)
		return
	}

	if err := s.store.SaveMixedContent(lib.ID, items, lib.UpdatedAt); err != nil {
		s.logger.Error("failed to save mixed content", "error", err, "libID", lib.ID)
	}

	observer.OnProgress(domain.SyncProgress{
		LibraryID: lib.ID, LibraryType: lib.Type,
		Loaded: len(items), Total: len(items), Done: true,
	})
	s.logger.Info("synced mixed content", "count", len(items), "libID", lib.ID)
}

func (s *LibraryService) reportCacheHit(lib domain.Library, observer domain.SyncObserver) {
	var count int
	switch lib.Type {
	case "movie":
		if movies, ok := s.store.GetMovies(lib.ID); ok {
			count = len(movies)
		}
	case "show":
		if shows, ok := s.store.GetShows(lib.ID); ok {
			count = len(shows)
		}
	default:
		if items, ok := s.store.GetMixedContent(lib.ID); ok {
			count = len(items)
		}
	}

	observer.OnProgress(domain.SyncProgress{
		LibraryID:   lib.ID,
		LibraryType: lib.Type,
		Loaded:      count,
		Total:       count,
		Done:        true,
		FromCache:   true,
	})
	s.logger.Debug("cache hit", "libID", lib.ID, "count", count)
}

// GetMovies returns all movies from a library (from store or network)
func (s *LibraryService) GetMovies(ctx context.Context, libID string) ([]*domain.MediaItem, error) {
	if movies, ok := s.store.GetMovies(libID); ok {
		s.logger.Debug("store hit for movies", "libID", libID)
		return movies, nil
	}

	movies, err := fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.MediaItem, int, error) {
			return s.repo.GetMovies(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		nil,
	)
	if err != nil {
		s.logger.Error("failed to get movies", "error", err, "libID", libID)
		return nil, err
	}

	s.store.SaveMovies(libID, movies, 0)
	s.logger.Info("loaded movies", "count", len(movies), "libID", libID)
	return movies, nil
}

// GetShows returns all TV shows from a library (from store or network)
func (s *LibraryService) GetShows(ctx context.Context, libID string) ([]*domain.Show, error) {
	if shows, ok := s.store.GetShows(libID); ok {
		s.logger.Debug("store hit for shows", "libID", libID)
		return shows, nil
	}

	shows, err := fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.Show, int, error) {
			return s.repo.GetShows(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		nil,
	)
	if err != nil {
		s.logger.Error("failed to get shows", "error", err, "libID", libID)
		return nil, err
	}

	s.store.SaveShows(libID, shows, 0)
	s.logger.Info("loaded shows", "count", len(shows), "libID", libID)
	return shows, nil
}

// GetLibraryContent returns all content from a mixed library (from store or network)
func (s *LibraryService) GetLibraryContent(ctx context.Context, libID string) ([]domain.ListItem, error) {
	if items, ok := s.store.GetMixedContent(libID); ok {
		s.logger.Debug("store hit for mixed content", "libID", libID)
		return items, nil
	}

	items, err := fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]domain.ListItem, int, error) {
			return s.repo.GetLibraryContent(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		nil,
	)
	if err != nil {
		s.logger.Error("failed to get library content", "error", err, "libID", libID)
		return nil, err
	}

	s.store.SaveMixedContent(libID, items, 0)
	s.logger.Info("loaded library content", "count", len(items), "libID", libID)
	return items, nil
}

// GetSeasons fetches seasons from network, caches in store.
// Requires libID for hierarchical cache key (enables cascade invalidation).
func (s *LibraryService) GetSeasons(ctx context.Context, libID, showID string) ([]*domain.Season, error) {
	if seasons, ok := s.store.GetSeasons(libID, showID); ok {
		s.logger.Debug("store hit for seasons", "libID", libID, "showID", showID)
		return seasons, nil
	}

	seasons, err := s.repo.GetSeasons(ctx, showID)
	if err != nil {
		s.logger.Error("failed to get seasons", "error", err, "showID", showID)
		return nil, err
	}

	s.store.SaveSeasons(libID, showID, seasons)
	s.logger.Debug("loaded seasons", "count", len(seasons), "showID", showID)
	return seasons, nil
}

// GetEpisodes fetches episodes from network, caches in store.
// Requires full ancestry (libID, showID, seasonID) for hierarchical cache key.
func (s *LibraryService) GetEpisodes(ctx context.Context, libID, showID, seasonID string) ([]*domain.MediaItem, error) {
	if episodes, ok := s.store.GetEpisodes(libID, showID, seasonID); ok {
		s.logger.Debug("store hit for episodes", "seasonID", seasonID)
		return episodes, nil
	}

	episodes, err := s.repo.GetEpisodes(ctx, seasonID)
	if err != nil {
		s.logger.Error("failed to get episodes", "error", err, "seasonID", seasonID)
		return nil, err
	}

	s.store.SaveEpisodes(libID, showID, seasonID, episodes)
	s.logger.Debug("loaded episodes", "count", len(episodes), "seasonID", seasonID)
	return episodes, nil
}

// RefreshLibrary invalidates library + all its seasons + all its episodes.
func (s *LibraryService) RefreshLibrary(libID string) {
	s.store.InvalidateLibrary(libID)
	s.logger.Info("invalidated cache for library (cascade)", "libID", libID)
}

// RefreshShow invalidates a show's seasons + all its episodes.
func (s *LibraryService) RefreshShow(libID, showID string) {
	s.store.InvalidateShow(libID, showID)
	s.logger.Info("invalidated cache for show (cascade)", "libID", libID, "showID", showID)
}

// RefreshSeason invalidates a season's episodes.
func (s *LibraryService) RefreshSeason(libID, showID, seasonID string) {
	s.store.InvalidateSeason(libID, showID, seasonID)
	s.logger.Info("invalidated cache for season", "seasonID", seasonID)
}

// RefreshAll invalidates all stored data.
func (s *LibraryService) RefreshAll() {
	s.store.InvalidateAll()
	s.logger.Info("invalidated all cache")
}
