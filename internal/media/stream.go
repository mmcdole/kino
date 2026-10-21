package media

import "time"

// Stream is what a player opens for one play of an item.
type Stream struct {
	PlayID string        // the server's play to report on; empty when the server keeps none
	Parts  []StreamPart  // in order; one for most media
	Start  time.Duration // where to start, on the parts' joined timeline
}

// StreamPart is one file or playlist of a stream.
type StreamPart struct {
	URL      string
	Duration time.Duration // zero when unknown, which only a single part may be
}

// PlayState is what the player is doing, as a progress report gives it.
type PlayState string

const (
	PlayPlaying   PlayState = "playing"
	PlayPaused    PlayState = "paused"
	PlayBuffering PlayState = "buffering"
	PlayEnded     PlayState = "ended"
)

// Progress is one report of where a play is.
type Progress struct {
	Position time.Duration
	State    PlayState
	// SeekToFirstFrame is, after a seek, how long the first frame took.
	SeekToFirstFrame time.Duration
}
