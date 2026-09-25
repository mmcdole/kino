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

// Player resolves an item's stream URL and opens it in an external player.
type Player struct {
	launcher *launcher
	resolver URLResolver
	logger   *slog.Logger
}

// New returns a Player that launches command with args, or detects an
// installed player when command is empty. seekFlag overrides the known
// resume flag for command (e.g. "--start=%d").
func New(command string, args []string, seekFlag string, resolver URLResolver, logger *slog.Logger) *Player {
	if logger == nil {
		logger = slog.Default()
	}
	return &Player{
		launcher: newLauncher(command, args, seekFlag, logger),
		resolver: resolver,
		logger:   logger,
	}
}

// Play starts playback of a media item from the beginning
func (s *Player) Play(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, 0)
}

// Resume starts playback from the saved position
func (s *Player) Resume(ctx context.Context, item domain.MediaItem) error {
	return s.playItem(ctx, item, item.ViewOffset)
}

// playItem resolves URL and launches player
func (s *Player) playItem(ctx context.Context, item domain.MediaItem, offset time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	url, err := s.resolver.ResolvePlayableURL(ctx, item.ID)
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
