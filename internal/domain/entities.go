package domain

import "time"

// MediaType distinguishes content types
type MediaType int

const (
	MediaTypeMovie MediaType = iota
	MediaTypeShow
	MediaTypeSeason
	MediaTypeEpisode
)

// MediaItem represents a playable item (Movie or Episode)
type MediaItem struct {
	ID         string        // Server-specific unique identifier
	Title      string        // Display title
	SortTitle  string        // Title used for sorting
	LibraryID  string        // Parent library ID
	Summary    string        // Plot synopsis
	Year       int           // Release year
	AddedAt    int64         // Unix timestamp when added to library
	UpdatedAt  int64         // Unix timestamp when last updated
	Duration   time.Duration // Total runtime
	ViewOffset time.Duration // Watch progress
	IsPlayed   bool          // Whether item is marked as watched
	Type       MediaType     // Movie or Episode

	// Episode-specific fields (empty for movies)
	ShowTitle  string // Parent show name
	ShowID     string // Parent show ID (for navigation)
	SeasonNum  int    // Season number (0 = specials)
	EpisodeNum int    // Episode number within season
	ParentID   string // Season ID (for navigation)

	// Rating (0-10 scale, audience/community rating)
	Rating float64

	// Content rating (e.g., "PG-13", "R", "TV-MA")
	ContentRating string

	// Technical metadata
	FileSize      int64  // File size in bytes
	Bitrate       int    // Bitrate in kbps
	Width         int    // Video width in pixels
	Height        int    // Video height in pixels
	VideoCodec    string // Normalized: "HEVC", "H.264", "AV1"
	AudioCodec    string // Normalized: "AAC", "AC3", "DTS"
	AudioChannels int    // Channel count: 2, 6, 8
	Container     string // "mkv", "mp4"

	// Image URLs
	ThumbURL string // Poster/thumbnail image URL
	ArtURL   string // Background art URL
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

// ShouldResume returns true if playback should resume from saved position
func (m MediaItem) ShouldResume() bool {
	return m.ViewOffset > 0 && !m.IsPlayed
}

// ListItem interface implementation for MediaItem

func (m *MediaItem) GetID() string    { return m.ID }
func (m *MediaItem) GetTitle() string { return m.Title }

// Show represents a TV series container
type Show struct {
	ID             string // Server-specific unique identifier
	Title          string // Series title
	SortTitle      string // Title used for sorting
	LibraryID      string // Parent library ID
	Summary        string // Series synopsis
	Year           int    // First air year
	AddedAt        int64  // Unix timestamp when added to library
	UpdatedAt      int64  // Unix timestamp when last updated
	SeasonCount    int    // Total number of seasons
	EpisodeCount   int    // Total number of episodes
	UnwatchedCount int    // Number of unwatched episodes

	// Rating (0-10 scale, audience/community rating)
	Rating float64

	// Content rating (e.g., "TV-MA", "TV-PG")
	ContentRating string

	// Image URLs
	ThumbURL string // Poster/thumbnail image URL
	ArtURL   string // Background art URL
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

// ListItem interface implementation for Show

func (s *Show) GetID() string    { return s.ID }
func (s *Show) GetTitle() string { return s.Title }

// Season represents a season container
type Season struct {
	ID             string // Server-specific unique identifier
	ShowID         string // Parent show ID
	ShowTitle      string // Parent show name
	SeasonNum      int    // Season number (0 = Specials)
	Title          string // "Season 1" or custom name
	EpisodeCount   int    // Total number of episodes
	UnwatchedCount int    // Number of unwatched episodes

	// Image URLs
	ThumbURL string // Poster/thumbnail image URL
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

// ListItem interface implementation for Season

func (s *Season) GetID() string    { return s.ID }
func (s *Season) GetTitle() string { return s.Title }

// Library represents a media server library section
type Library struct {
	ID        string // Server-specific unique identifier
	Name      string // Display name
	Type      string // "movie" or "show"
	UpdatedAt int64  // Server's contentChangedAt timestamp
}

// ListItem interface implementation for Library

func (l *Library) GetID() string    { return l.ID }
func (l *Library) GetTitle() string { return l.Name }

// Playlist represents a user-created playlist
type Playlist struct {
	ID           string        // Playlist identifier
	Title        string        // Display title
	PlaylistType string        // "video", "audio", "photo"
	Smart        bool          // Smart/dynamic playlist
	ItemCount    int           // Number of items in playlist
	Duration     time.Duration // Total duration of all items
	UpdatedAt    int64         // Unix timestamp when last updated
}

// ListItem interface implementation for Playlist

func (p *Playlist) GetID() string    { return p.ID }
func (p *Playlist) GetTitle() string { return p.Title }

// WatchStatus represents the viewing state of media
type WatchStatus int

const (
	WatchStatusUnwatched WatchStatus = iota
	WatchStatusInProgress
	WatchStatusWatched
)
