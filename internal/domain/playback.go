package domain

import "context"

// PlaybackClient provides network operations for media playback.
type PlaybackClient interface {
	ResolvePlayableURL(ctx context.Context, itemID string) (string, error)
	MarkPlayed(ctx context.Context, itemID string) error
	MarkUnplayed(ctx context.Context, itemID string) error
}
