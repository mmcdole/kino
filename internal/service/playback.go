package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// launcher abstracts media player launching (consumer-defined interface)
type launcher interface {
	Launch(url string, startOffset time.Duration) error
}

// PlaybackService orchestrates playback operations
type PlaybackService struct {
	launcher launcher
	metadata domain.MetadataRepository
	logger   *slog.Logger
}

// NewPlaybackService creates a new playback service
func NewPlaybackService(
	launcher launcher,
	metadata domain.MetadataRepository,
	logger *slog.Logger,
) *PlaybackService {
	if logger == nil {
		logger = slog.Default()
	}
	return &PlaybackService{
		launcher: launcher,
		metadata: metadata,
		logger:   logger,
	}
}

// Play starts playback of a media item from the beginning
func (s *PlaybackService) Play(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, 0)
}

// Resume starts playback from the saved position
func (s *PlaybackService) Resume(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, item.ViewOffset)
}

// playItem resolves URL and launches player
func (s *PlaybackService) playItem(ctx context.Context, item domain.MediaItem, offset time.Duration) error {
	url, err := s.metadata.ResolvePlayableURL(ctx, item.ID)
	if err != nil {
		s.logger.Error("failed to resolve playable URL", "error", err, "itemID", item.ID)
		return err
	}

	s.logger.Info("launching playback", "title", item.Title, "itemID", item.ID, "offset", offset)

	return s.launcher.Launch(url, offset)
}

// MarkWatched marks an item as fully watched
func (s *PlaybackService) MarkWatched(ctx context.Context, itemID string) error {
	return s.metadata.MarkPlayed(ctx, itemID)
}

// MarkUnwatched marks an item as unwatched
func (s *PlaybackService) MarkUnwatched(ctx context.Context, itemID string) error {
	return s.metadata.MarkUnplayed(ctx, itemID)
}
