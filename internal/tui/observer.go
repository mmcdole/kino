package tui

import "github.com/mmcdole/kino/internal/domain"

// ChannelObserver adapts domain.SyncObserver to a channel for Bubble Tea.
type ChannelObserver struct {
	ch chan<- domain.SyncProgress
}

// NewChannelObserver creates a new channel-based observer.
func NewChannelObserver(ch chan<- domain.SyncProgress) *ChannelObserver {
	return &ChannelObserver{ch: ch}
}

// OnProgress sends progress to the channel (non-blocking if full).
func (o *ChannelObserver) OnProgress(progress domain.SyncProgress) {
	select {
	case o.ch <- progress:
	default: // Non-blocking if channel full
	}
}
