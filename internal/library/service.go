package library

import (
	"context"
	"log/slog"

	"github.com/mmcdole/kino/internal/domain"
)

const defaultChunkSize = 50

// Service orchestrates library client + store operations.
type Service struct {
	client domain.LibraryClient
	store  domain.Store
	logger *slog.Logger
}

// NewService creates a new library service.
func NewService(client domain.LibraryClient, store domain.Store, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{client: client, store: store, logger: logger}
}

func (s *Service) FetchLibraries(ctx context.Context) ([]domain.Library, error) {
	libs, err := s.client.GetLibraries(ctx)
	if err != nil {
		s.logger.Error("failed to fetch libraries", "error", err)
		return nil, err
	}
	if err := s.store.SaveLibraries(libs); err != nil {
		s.logger.Error("failed to save libraries", "error", err)
	}
	s.logger.Debug("fetched libraries", "count", len(libs))
	return libs, nil
}

func (s *Service) SyncLibrary(
	ctx context.Context,
	lib domain.Library,
	onProgress domain.ProgressFunc,
) (domain.SyncResult, error) {
	// 1. Freshness check. The library timestamp alone is not enough: servers
	// don't reliably bump it when items are added (Jellyfin's Views only
	// expose the library's creation date), so also verify the item count
	// with a cheap metadata-only request.
	if s.store.IsValid(lib.ID, lib.UpdatedAt) {
		count := s.getCachedCount(lib)
		serverCount, err := s.client.GetLibraryItemCount(ctx, lib.ID, lib.Type)
		if err != nil {
			// Can't verify; serve cache rather than fail or refetch blindly
			s.logger.Warn("item count check failed, serving cache", "libID", lib.ID, "error", err)
			return domain.SyncResult{LibraryID: lib.ID, FromCache: true, Count: count}, nil
		}
		if serverCount == count {
			s.logger.Debug("cache fresh", "libID", lib.ID, "count", count)
			return domain.SyncResult{LibraryID: lib.ID, FromCache: true, Count: count}, nil
		}
		s.logger.Debug("item count changed", "libID", lib.ID, "cached", count, "server", serverCount)
	}

	// 2. Fetch based on library type
	s.logger.Debug("cache stale, fetching", "libID", lib.ID)

	switch lib.Type {
	case "movie":
		movies, err := s.fetchMoviesWithProgress(ctx, lib.ID, onProgress)
		if err != nil {
			return domain.SyncResult{}, err
		}
		if err := s.store.SaveMovies(lib.ID, movies, lib.UpdatedAt); err != nil {
			s.logger.Error("failed to save movies", "error", err, "libID", lib.ID)
		}
		return domain.SyncResult{LibraryID: lib.ID, FromCache: false, Count: len(movies)}, nil

	case "show":
		shows, err := s.fetchShowsWithProgress(ctx, lib.ID, onProgress)
		if err != nil {
			return domain.SyncResult{}, err
		}
		if err := s.store.SaveShows(lib.ID, shows, lib.UpdatedAt); err != nil {
			s.logger.Error("failed to save shows", "error", err, "libID", lib.ID)
		}
		return domain.SyncResult{LibraryID: lib.ID, FromCache: false, Count: len(shows)}, nil

	default: // mixed
		items, err := s.fetchMixedWithProgress(ctx, lib.ID, onProgress)
		if err != nil {
			return domain.SyncResult{}, err
		}
		if err := s.store.SaveMixedContent(lib.ID, items, lib.UpdatedAt); err != nil {
			s.logger.Error("failed to save mixed content", "error", err, "libID", lib.ID)
		}
		return domain.SyncResult{LibraryID: lib.ID, FromCache: false, Count: len(items)}, nil
	}
}

// Fetch* fetch a library's full content and cache it. serverTS is the
// library's UpdatedAt as known to the caller: the cache timestamp must hold
// the server's library version, never the local clock — a local timestamp
// compares as "newer than the server" forever and permanently disables
// timestamp invalidation.

func (s *Service) FetchMovies(
	ctx context.Context,
	libID string,
	serverTS int64,
	onProgress domain.ProgressFunc,
) ([]*domain.MediaItem, error) {
	movies, err := s.fetchMoviesWithProgress(ctx, libID, onProgress)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveMovies(libID, movies, serverTS); err != nil {
		s.logger.Error("failed to save movies", "error", err, "libID", libID)
	}
	s.logger.Debug("fetched movies", "count", len(movies), "libID", libID)
	return movies, nil
}

func (s *Service) FetchShows(
	ctx context.Context,
	libID string,
	serverTS int64,
	onProgress domain.ProgressFunc,
) ([]*domain.Show, error) {
	shows, err := s.fetchShowsWithProgress(ctx, libID, onProgress)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveShows(libID, shows, serverTS); err != nil {
		s.logger.Error("failed to save shows", "error", err, "libID", libID)
	}
	s.logger.Debug("fetched shows", "count", len(shows), "libID", libID)
	return shows, nil
}

func (s *Service) FetchMixedContent(
	ctx context.Context,
	libID string,
	serverTS int64,
	onProgress domain.ProgressFunc,
) ([]domain.ListItem, error) {
	items, err := s.fetchMixedWithProgress(ctx, libID, onProgress)
	if err != nil {
		return nil, err
	}
	if err := s.store.SaveMixedContent(libID, items, serverTS); err != nil {
		s.logger.Error("failed to save mixed content", "error", err, "libID", libID)
	}
	s.logger.Debug("fetched mixed content", "count", len(items), "libID", libID)
	return items, nil
}

func (s *Service) FetchSeasons(ctx context.Context, libID, showID string) ([]*domain.Season, error) {
	seasons, err := s.client.GetSeasons(ctx, showID)
	if err != nil {
		s.logger.Error("failed to fetch seasons", "error", err, "showID", showID)
		return nil, err
	}
	if err := s.store.SaveSeasons(libID, showID, seasons); err != nil {
		s.logger.Error("failed to save seasons", "error", err, "showID", showID)
	}
	s.logger.Debug("fetched seasons", "count", len(seasons), "showID", showID)
	return seasons, nil
}

func (s *Service) FetchEpisodes(ctx context.Context, libID, showID, seasonID string) ([]*domain.MediaItem, error) {
	episodes, err := s.client.GetEpisodes(ctx, seasonID)
	if err != nil {
		s.logger.Error("failed to fetch episodes", "error", err, "seasonID", seasonID)
		return nil, err
	}
	if err := s.store.SaveEpisodes(libID, showID, seasonID, episodes); err != nil {
		s.logger.Error("failed to save episodes", "error", err, "seasonID", seasonID)
	}
	s.logger.Debug("fetched episodes", "count", len(episodes), "seasonID", seasonID)
	return episodes, nil
}

// SetWatchState patches the cached watch state for an item in place,
// avoiding cache invalidation. The next sync reconciles with the server.
func (s *Service) SetWatchState(itemID string, played bool) {
	s.store.SetWatchState(itemID, played)
	s.logger.Debug("patched cached watch state", "itemID", itemID, "played", played)
}

func (s *Service) InvalidateLibrary(libID string) {
	s.store.InvalidateLibrary(libID)
	s.logger.Info("invalidated library cache", "libID", libID)
}

func (s *Service) InvalidateShow(libID, showID string) {
	s.store.InvalidateShow(libID, showID)
	s.logger.Info("invalidated show cache", "libID", libID, "showID", showID)
}

func (s *Service) InvalidateSeason(libID, showID, seasonID string) {
	s.store.InvalidateSeason(libID, showID, seasonID)
	s.logger.Info("invalidated season cache", "seasonID", seasonID)
}

func (s *Service) InvalidateAll() {
	s.store.InvalidateAll()
	s.logger.Info("invalidated all cache")
}

// --- Private helpers ---

func (s *Service) getCachedCount(lib domain.Library) int {
	switch lib.Type {
	case "movie":
		if movies, ok := s.store.GetMovies(lib.ID); ok {
			return len(movies)
		}
	case "show":
		if shows, ok := s.store.GetShows(lib.ID); ok {
			return len(shows)
		}
	default:
		if items, ok := s.store.GetMixedContent(lib.ID); ok {
			return len(items)
		}
	}
	return 0
}

func (s *Service) fetchMoviesWithProgress(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]*domain.MediaItem, error) {
	return fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.MediaItem, int, error) {
			return s.client.GetMovies(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		onProgress,
	)
}

func (s *Service) fetchShowsWithProgress(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]*domain.Show, error) {
	return fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.Show, int, error) {
			return s.client.GetShows(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		onProgress,
	)
}

func (s *Service) fetchMixedWithProgress(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]domain.ListItem, error) {
	return fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]domain.ListItem, int, error) {
			return s.client.GetMixedContent(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		onProgress,
	)
}

// fetchAll is a generic pagination helper. Items are deduplicated by ID:
// offset pagination under concurrent server-side mutation can shift pages
// and repeat items, and duplicates would otherwise be cached as truth. An
// empty page always terminates, so a server reporting total=0 alongside a
// non-empty page still gets fully paginated rather than truncated.
func fetchAll[T domain.ListItem](
	ctx context.Context,
	fetch func(ctx context.Context, offset, limit int) ([]T, int, error),
	chunkSize int,
	onProgress domain.ProgressFunc,
) ([]T, error) {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	var all []T
	seen := make(map[string]bool)
	offset := 0

	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		items, total, err := fetch(ctx, offset, chunkSize)
		if err != nil {
			return nil, err
		}

		for _, item := range items {
			id := item.GetID()
			if id != "" && seen[id] {
				continue
			}
			seen[id] = true
			all = append(all, item)
		}

		if onProgress != nil {
			onProgress(len(all), total)
		}

		if len(items) == 0 || (total > 0 && len(all) >= total) {
			break
		}
		offset += chunkSize
	}

	return all, nil
}
