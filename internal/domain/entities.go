package domain

import (
	"fmt"
	"time"
)

// MediaType distinguishes content types
type MediaType int

const (
	MediaTypeMovie MediaType = iota
	MediaTypeShow
	MediaTypeSeason
	MediaTypeEpisode
)

// String returns a human-readable representation of the media type
func (m MediaType) String() string {
	switch m {
	case MediaTypeMovie:
		return "Movie"
	case MediaTypeShow:
		return "Show"
	case MediaTypeSeason:
		return "Season"
	case MediaTypeEpisode:
		return "Episode"
	default:
		return "Unknown"
	}
}

// MediaItem represents a playable item (Movie or Episode)
type MediaItem struct {
	ID         string        // Plex RatingKey
	Title      string        // Display title
	SortTitle  string        // Title used for sorting
	LibraryID  string        // Parent library ID
	Summary    string        // Plot synopsis
	Year       int           // Release year
	Duration   time.Duration // Total runtime
	ViewOffset time.Duration // Watch progress
	ThumbURL   string        // Thumbnail image URL
	MediaURL   string        // Direct playback URL
	Format     string        // "HEVC", "H.264", etc.
	IsPlayed   bool          // Whether item is marked as watched
	Type       MediaType     // Movie or Episode

	// Episode-specific fields (empty for movies)
	ShowTitle  string // Parent show name
	ShowID     string // Parent show ID (for navigation)
	SeasonNum  int    // Season number (0 = specials)
	EpisodeNum int    // Episode number within season
	ParentID   string // Season ID (for navigation)
}

// WatchStatus returns the watch status of the media item
func (m MediaItem) WatchStatus() WatchStatus {
	if m.IsPlayed {
		return WatchStatusWatched
	}
	if m.ViewOffset > 0 {
		return WatchStatusInProgress
	}
	return WatchStatusUnwatched
}

// ProgressPercent returns the watch progress as a percentage (0-100)
func (m MediaItem) ProgressPercent() float64 {
	if m.Duration == 0 {
		return 0
	}
	return float64(m.ViewOffset) / float64(m.Duration) * 100
}

// FormattedDuration returns the duration in a human-readable format
func (m MediaItem) FormattedDuration() string {
	h := int(m.Duration.Hours())
	mins := int(m.Duration.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// EpisodeCode returns the formatted episode code (e.g., "S01E05")
func (m MediaItem) EpisodeCode() string {
	if m.Type != MediaTypeEpisode {
		return ""
	}
	return fmt.Sprintf("S%02dE%02d", m.SeasonNum, m.EpisodeNum)
}

// Show represents a TV series container
type Show struct {
	ID             string // Plex RatingKey
	Title          string // Series title
	SortTitle      string // Title used for sorting
	LibraryID      string // Parent library ID
	Summary        string // Series synopsis
	Year           int    // First air year
	ThumbURL       string // Thumbnail image URL
	SeasonCount    int    // Total number of seasons
	EpisodeCount   int    // Total number of episodes
	UnwatchedCount int    // Number of unwatched episodes
}

// WatchStatus returns the watch status of the show
func (s Show) WatchStatus() WatchStatus {
	if s.UnwatchedCount == 0 {
		return WatchStatusWatched
	}
	if s.UnwatchedCount < s.EpisodeCount {
		return WatchStatusInProgress
	}
	return WatchStatusUnwatched
}

// ProgressPercent returns the watch progress as a percentage (0-100)
func (s Show) ProgressPercent() float64 {
	if s.EpisodeCount == 0 {
		return 0
	}
	watched := s.EpisodeCount - s.UnwatchedCount
	return float64(watched) / float64(s.EpisodeCount) * 100
}

// Season represents a season container
type Season struct {
	ID             string // Plex RatingKey
	ShowID         string // Parent show ID
	ShowTitle      string // Parent show name
	SeasonNum      int    // Season number (0 = Specials)
	Title          string // "Season 1" or custom name
	ThumbURL       string // Thumbnail image URL
	EpisodeCount   int    // Total number of episodes
	UnwatchedCount int    // Number of unwatched episodes
}

// WatchStatus returns the watch status of the season
func (s Season) WatchStatus() WatchStatus {
	if s.UnwatchedCount == 0 {
		return WatchStatusWatched
	}
	if s.UnwatchedCount < s.EpisodeCount {
		return WatchStatusInProgress
	}
	return WatchStatusUnwatched
}

// ProgressPercent returns the watch progress as a percentage (0-100)
func (s Season) ProgressPercent() float64 {
	if s.EpisodeCount == 0 {
		return 0
	}
	watched := s.EpisodeCount - s.UnwatchedCount
	return float64(watched) / float64(s.EpisodeCount) * 100
}

// DisplayTitle returns the display title for the season
func (s Season) DisplayTitle() string {
	if s.SeasonNum == 0 {
		return "Specials"
	}
	if s.Title != "" && s.Title != fmt.Sprintf("Season %d", s.SeasonNum) {
		return fmt.Sprintf("Season %d: %s", s.SeasonNum, s.Title)
	}
	return fmt.Sprintf("Season %d", s.SeasonNum)
}

// Library represents a Plex library section
type Library struct {
	ID        string // Plex section key
	Name      string // Display name
	Type      string // "movie" or "show"
	UpdatedAt int64  // Server's contentChangedAt timestamp
}

// IsMovieLibrary returns true if this is a movie library
func (l Library) IsMovieLibrary() bool {
	return l.Type == "movie"
}

// IsShowLibrary returns true if this is a TV show library
func (l Library) IsShowLibrary() bool {
	return l.Type == "show"
}

// PlayerStatus represents the current state of the media player
type PlayerStatus struct {
	CurrentTime time.Duration // Current playback position
	TotalTime   time.Duration // Total media duration
	IsPaused    bool          // Whether playback is paused
	IsBuffering bool          // Whether player is buffering
	IsStopped   bool          // Whether player has stopped
	FilePath    string        // Currently playing file
}

// ProgressPercent returns the playback progress as a percentage (0-100)
func (p PlayerStatus) ProgressPercent() float64 {
	if p.TotalTime == 0 {
		return 0
	}
	return float64(p.CurrentTime) / float64(p.TotalTime) * 100
}

// RemainingTime returns the remaining playback time
func (p PlayerStatus) RemainingTime() time.Duration {
	if p.CurrentTime >= p.TotalTime {
		return 0
	}
	return p.TotalTime - p.CurrentTime
}

// WatchStatus represents the viewing state of media
type WatchStatus int

const (
	WatchStatusUnwatched WatchStatus = iota
	WatchStatusInProgress
	WatchStatusWatched
)

// String returns a human-readable representation of the watch status
func (w WatchStatus) String() string {
	switch w {
	case WatchStatusUnwatched:
		return "Unwatched"
	case WatchStatusInProgress:
		return "In Progress"
	case WatchStatusWatched:
		return "Watched"
	default:
		return "Unknown"
	}
}

