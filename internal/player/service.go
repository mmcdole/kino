package player

import (
	"context"
	"log/slog"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// URLResolver is the backend operation playback consumes.
type URLResolver interface {
	ResolvePlayableURL(context.Context, string) (string, error)
}

// Service orchestrates playback operations
type Service struct {
	launcher *Launcher
	playback URLResolver
	logger   *slog.Logger
}

// NewService creates a new playback service
func NewService(launcher *Launcher, playback URLResolver, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		launcher: launcher,
		playback: playback,
		logger:   logger,
	}
}

// Play starts playback of a media item from the beginning
func (s *Service) Play(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, 0)
}

// Resume starts playback from the saved position
func (s *Service) Resume(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, item.ViewOffset)
}

// playItem resolves URL and launches player
func (s *Service) playItem(ctx context.Context, item domain.MediaItem, offset time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	url, err := s.playback.ResolvePlayableURL(ctx, item.ID)
	if err != nil {
		s.logger.Error("failed to resolve playable URL", "error", err, "itemID", item.ID)
		return err
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	s.logger.Info("launching playback", "title", item.Title, "itemID", item.ID, "offset", offset)

	return s.launcher.Launch(ctx, url, offset)
}
