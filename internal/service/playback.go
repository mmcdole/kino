package service

import (
	"context"
	"log/slog"
	"time"

	"github.com/mmcdole/kino/internal/adapter"
	"github.com/mmcdole/kino/internal/domain"
)

// PlaybackService orchestrates playback operations
type PlaybackService struct {
	launcher *adapter.Launcher
	metadata domain.MetadataRepository
	logger   *slog.Logger
}

// NewPlaybackService creates a new playback service
func NewPlaybackService(
	launcher *adapter.Launcher,
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

// HasViewOffset returns true if the item has a saved position to resume from
func HasViewOffset(item domain.MediaItem) bool {
	return item.ViewOffset > 0
}

// FormatViewOffset returns a human-readable string for the view offset
func FormatViewOffset(offset time.Duration) string {
	hours := int(offset.Hours())
	minutes := int(offset.Minutes()) % 60
	seconds := int(offset.Seconds()) % 60

	if hours > 0 {
		return formatTime(hours, minutes, seconds)
	}
	return formatTimeShort(minutes, seconds)
}

func formatTime(h, m, s int) string {
	return formatNum(h) + ":" + formatNum(m) + ":" + formatNum(s)
}

func formatTimeShort(m, s int) string {
	return formatNum(m) + ":" + formatNum(s)
}

func formatNum(n int) string {
	if n < 10 {
		return "0" + string(rune('0'+n))
	}
	return string(rune('0'+n/10)) + string(rune('0'+n%10))
}
