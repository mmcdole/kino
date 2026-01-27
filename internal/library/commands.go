package library

import (
	"context"
	"log/slog"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

const defaultChunkSize = 50

// Commands provides asynchronous operations that hit network.
// Implements domain.LibraryCommands.
type Commands struct {
	repo   domain.LibraryRepository
	store  domain.Store
	logger *slog.Logger
}

// NewCommands creates a new Commands instance.
func NewCommands(repo domain.LibraryRepository, store domain.Store, logger *slog.Logger) *Commands {
	if logger == nil {
		logger = slog.Default()
	}
	return &Commands{repo: repo, store: store, logger: logger}
}

func (c *Commands) FetchLibraries(ctx context.Context) ([]domain.Library, error) {
	libs, err := c.repo.GetLibraries(ctx)
	if err != nil {
		c.logger.Error("failed to fetch libraries", "error", err)
		return nil, err
	}
	if err := c.store.SaveLibraries(libs); err != nil {
		c.logger.Error("failed to save libraries", "error", err)
	}
	c.logger.Debug("fetched libraries", "count", len(libs))
	return libs, nil
}

func (c *Commands) SyncLibrary(
	ctx context.Context,
	lib domain.Library,
	onProgress domain.ProgressFunc,
) (domain.SyncResult, error) {
	// 1. Freshness check
	if c.store.IsValid(lib.ID, lib.UpdatedAt) {
		count := c.getCachedCount(lib)
		c.logger.Debug("cache fresh", "libID", lib.ID, "count", count)
		return domain.SyncResult{LibraryID: lib.ID, FromCache: true, Count: count}, nil
	}

	// 2. Fetch based on library type
	c.logger.Debug("cache stale, fetching", "libID", lib.ID)

	switch lib.Type {
	case "movie":
		movies, err := c.fetchMoviesWithProgress(ctx, lib.ID, onProgress)
		if err != nil {
			return domain.SyncResult{}, err
		}
		if err := c.store.SaveMovies(lib.ID, movies, lib.UpdatedAt); err != nil {
			c.logger.Error("failed to save movies", "error", err, "libID", lib.ID)
		}
		return domain.SyncResult{LibraryID: lib.ID, FromCache: false, Count: len(movies)}, nil

	case "show":
		shows, err := c.fetchShowsWithProgress(ctx, lib.ID, onProgress)
		if err != nil {
			return domain.SyncResult{}, err
		}
		if err := c.store.SaveShows(lib.ID, shows, lib.UpdatedAt); err != nil {
			c.logger.Error("failed to save shows", "error", err, "libID", lib.ID)
		}
		return domain.SyncResult{LibraryID: lib.ID, FromCache: false, Count: len(shows)}, nil

	default: // mixed
		items, err := c.fetchMixedWithProgress(ctx, lib.ID, onProgress)
		if err != nil {
			return domain.SyncResult{}, err
		}
		if err := c.store.SaveMixedContent(lib.ID, items, lib.UpdatedAt); err != nil {
			c.logger.Error("failed to save mixed content", "error", err, "libID", lib.ID)
		}
		return domain.SyncResult{LibraryID: lib.ID, FromCache: false, Count: len(items)}, nil
	}
}

func (c *Commands) FetchMovies(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]*domain.MediaItem, error) {
	movies, err := c.fetchMoviesWithProgress(ctx, libID, onProgress)
	if err != nil {
		return nil, err
	}
	if err := c.store.SaveMovies(libID, movies, time.Now().Unix()); err != nil {
		c.logger.Error("failed to save movies", "error", err, "libID", libID)
	}
	c.logger.Debug("fetched movies", "count", len(movies), "libID", libID)
	return movies, nil
}

func (c *Commands) FetchShows(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]*domain.Show, error) {
	shows, err := c.fetchShowsWithProgress(ctx, libID, onProgress)
	if err != nil {
		return nil, err
	}
	if err := c.store.SaveShows(libID, shows, time.Now().Unix()); err != nil {
		c.logger.Error("failed to save shows", "error", err, "libID", libID)
	}
	c.logger.Debug("fetched shows", "count", len(shows), "libID", libID)
	return shows, nil
}

func (c *Commands) FetchMixedContent(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]domain.ListItem, error) {
	items, err := c.fetchMixedWithProgress(ctx, libID, onProgress)
	if err != nil {
		return nil, err
	}
	if err := c.store.SaveMixedContent(libID, items, time.Now().Unix()); err != nil {
		c.logger.Error("failed to save mixed content", "error", err, "libID", libID)
	}
	c.logger.Debug("fetched mixed content", "count", len(items), "libID", libID)
	return items, nil
}

func (c *Commands) FetchSeasons(ctx context.Context, libID, showID string) ([]*domain.Season, error) {
	seasons, err := c.repo.GetSeasons(ctx, showID)
	if err != nil {
		c.logger.Error("failed to fetch seasons", "error", err, "showID", showID)
		return nil, err
	}
	if err := c.store.SaveSeasons(libID, showID, seasons); err != nil {
		c.logger.Error("failed to save seasons", "error", err, "showID", showID)
	}
	c.logger.Debug("fetched seasons", "count", len(seasons), "showID", showID)
	return seasons, nil
}

func (c *Commands) FetchEpisodes(ctx context.Context, libID, showID, seasonID string) ([]*domain.MediaItem, error) {
	episodes, err := c.repo.GetEpisodes(ctx, seasonID)
	if err != nil {
		c.logger.Error("failed to fetch episodes", "error", err, "seasonID", seasonID)
		return nil, err
	}
	if err := c.store.SaveEpisodes(libID, showID, seasonID, episodes); err != nil {
		c.logger.Error("failed to save episodes", "error", err, "seasonID", seasonID)
	}
	c.logger.Debug("fetched episodes", "count", len(episodes), "seasonID", seasonID)
	return episodes, nil
}

func (c *Commands) InvalidateLibrary(libID string) {
	c.store.InvalidateLibrary(libID)
	c.logger.Info("invalidated library cache", "libID", libID)
}

func (c *Commands) InvalidateShow(libID, showID string) {
	c.store.InvalidateShow(libID, showID)
	c.logger.Info("invalidated show cache", "libID", libID, "showID", showID)
}

func (c *Commands) InvalidateSeason(libID, showID, seasonID string) {
	c.store.InvalidateSeason(libID, showID, seasonID)
	c.logger.Info("invalidated season cache", "seasonID", seasonID)
}

func (c *Commands) InvalidateAll() {
	c.store.InvalidateAll()
	c.logger.Info("invalidated all cache")
}

// --- Private helpers ---

func (c *Commands) getCachedCount(lib domain.Library) int {
	switch lib.Type {
	case "movie":
		if movies, ok := c.store.GetMovies(lib.ID); ok {
			return len(movies)
		}
	case "show":
		if shows, ok := c.store.GetShows(lib.ID); ok {
			return len(shows)
		}
	default:
		if items, ok := c.store.GetMixedContent(lib.ID); ok {
			return len(items)
		}
	}
	return 0
}

func (c *Commands) fetchMoviesWithProgress(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]*domain.MediaItem, error) {
	return fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.MediaItem, int, error) {
			return c.repo.GetMovies(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		onProgress,
	)
}

func (c *Commands) fetchShowsWithProgress(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]*domain.Show, error) {
	return fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]*domain.Show, int, error) {
			return c.repo.GetShows(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		onProgress,
	)
}

func (c *Commands) fetchMixedWithProgress(
	ctx context.Context,
	libID string,
	onProgress domain.ProgressFunc,
) ([]domain.ListItem, error) {
	return fetchAll(ctx,
		func(ctx context.Context, offset, limit int) ([]domain.ListItem, int, error) {
			return c.repo.GetLibraryContent(ctx, libID, offset, limit)
		},
		defaultChunkSize,
		onProgress,
	)
}

// fetchAll is a generic pagination helper.
func fetchAll[T any](
	ctx context.Context,
	fetch func(ctx context.Context, offset, limit int) ([]T, int, error),
	chunkSize int,
	onProgress domain.ProgressFunc,
) ([]T, error) {
	if chunkSize <= 0 {
		chunkSize = defaultChunkSize
	}

	var all []T
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

		all = append(all, items...)

		if onProgress != nil {
			onProgress(len(all), total)
		}

		if len(all) >= total || len(items) == 0 {
			break
		}
		offset += chunkSize
	}

	return all, nil
}
