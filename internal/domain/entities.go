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

// Resolution returns a human-readable resolution string based on video height
func (m MediaItem) Resolution() string {
	switch {
	case m.Height >= 2160:
		return "4K"
	case m.Height >= 1080:
		return "1080p"
	case m.Height >= 720:
		return "720p"
	case m.Height >= 480:
		return "480p"
	case m.Height > 0:
		return fmt.Sprintf("%dp", m.Height)
	default:
		return ""
	}
}

// FormattedFileSize returns the file size in a human-readable format
func (m MediaItem) FormattedFileSize() string {
	if m.FileSize <= 0 {
		return ""
	}
	const (
		gb = 1024 * 1024 * 1024
		mb = 1024 * 1024
	)
	switch {
	case m.FileSize >= gb:
		return fmt.Sprintf("%.1f GB", float64(m.FileSize)/float64(gb))
	default:
		return fmt.Sprintf("%d MB", m.FileSize/mb)
	}
}

// ChannelLayout returns the audio channel layout as a string
func (m MediaItem) ChannelLayout() string {
	switch m.AudioChannels {
	case 8:
		return "7.1"
	case 6:
		return "5.1"
	case 2:
		return "Stereo"
	case 1:
		return "Mono"
	default:
		return ""
	}
}

// ListItem interface implementation for MediaItem

func (m *MediaItem) GetID() string    { return m.ID }
func (m *MediaItem) GetTitle() string { return m.Title }
func (m *MediaItem) GetSortTitle() string {
	if m.SortTitle != "" {
		return m.SortTitle
	}
	return m.Title
}
func (m *MediaItem) GetDuration() time.Duration  { return m.Duration }
func (m *MediaItem) GetRating() float64          { return m.Rating }
func (m *MediaItem) GetYear() int                { return m.Year }
func (m *MediaItem) GetAddedAt() int64           { return m.AddedAt }
func (m *MediaItem) GetUpdatedAt() int64         { return m.UpdatedAt }
func (m *MediaItem) GetWatchStatus() WatchStatus { return m.WatchStatus() }

func (m *MediaItem) GetItemType() string {
	switch m.Type {
	case MediaTypeMovie:
		return "movie"
	case MediaTypeEpisode:
		return "episode"
	default:
		return "unknown"
	}
}

func (m *MediaItem) GetDescription() string {
	if m.Type == MediaTypeEpisode {
		return m.FormattedDuration()
	}
	// For movies, show year if available
	if m.Year > 0 {
		return fmt.Sprintf("%d", m.Year)
	}
	return m.FormattedDuration()
}

func (m *MediaItem) CanDrillDown() bool {
	// Movies and episodes are leaf items - can't drill further
	return false
}

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
func (s *Show) GetSortTitle() string {
	if s.SortTitle != "" {
		return s.SortTitle
	}
	return s.Title
}
func (s *Show) GetDuration() time.Duration  { return 0 }
func (s *Show) GetRating() float64          { return s.Rating }
func (s *Show) GetYear() int                { return s.Year }
func (s *Show) GetAddedAt() int64           { return s.AddedAt }
func (s *Show) GetUpdatedAt() int64         { return s.UpdatedAt }
func (s *Show) GetItemType() string         { return "show" }
func (s *Show) GetWatchStatus() WatchStatus { return s.WatchStatus() }

func (s *Show) GetDescription() string {
	if s.SeasonCount == 1 {
		return fmt.Sprintf("%d Season", s.SeasonCount)
	}
	return fmt.Sprintf("%d Seasons", s.SeasonCount)
}

func (s *Show) CanDrillDown() bool {
	// Shows can be drilled into to see seasons
	return true
}

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

// ListItem interface implementation for Season

func (s *Season) GetID() string               { return s.ID }
func (s *Season) GetTitle() string            { return s.DisplayTitle() }
func (s *Season) GetSortTitle() string        { return fmt.Sprintf("%03d", s.SeasonNum) }
func (s *Season) GetDuration() time.Duration  { return 0 }
func (s *Season) GetRating() float64          { return 0 }
func (s *Season) GetYear() int                { return 0 } // Seasons don't have a year
func (s *Season) GetAddedAt() int64           { return 0 }
func (s *Season) GetUpdatedAt() int64         { return 0 }
func (s *Season) GetItemType() string         { return "season" }
func (s *Season) GetWatchStatus() WatchStatus { return s.WatchStatus() }

func (s *Season) GetDescription() string {
	if s.EpisodeCount == 1 {
		return "1 Episode"
	}
	return fmt.Sprintf("%d Episodes", s.EpisodeCount)
}

func (s *Season) CanDrillDown() bool {
	// Seasons can be drilled into to see episodes
	return true
}

// Library represents a media server library section
type Library struct {
	ID        string // Server-specific unique identifier
	Name      string // Display name
	Type      string // "movie" or "show"
	UpdatedAt int64  // Server's contentChangedAt timestamp
}

// ListItem interface implementation for Library

func (l *Library) GetID() string               { return l.ID }
func (l *Library) GetTitle() string            { return l.Name }
func (l *Library) GetSortTitle() string        { return l.Name }
func (l *Library) GetDuration() time.Duration  { return 0 }
func (l *Library) GetRating() float64          { return 0 }
func (l *Library) GetYear() int                { return 0 }
func (l *Library) GetAddedAt() int64           { return 0 }
func (l *Library) GetUpdatedAt() int64         { return l.UpdatedAt }
func (l *Library) GetItemType() string         { return "library" }
func (l *Library) GetWatchStatus() WatchStatus { return WatchStatusUnwatched }
func (l *Library) GetDescription() string      { return l.Type }
func (l *Library) CanDrillDown() bool          { return true }

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

func (p *Playlist) GetID() string               { return p.ID }
func (p *Playlist) GetTitle() string            { return p.Title }
func (p *Playlist) GetSortTitle() string        { return p.Title }
func (p *Playlist) GetDuration() time.Duration  { return p.Duration }
func (p *Playlist) GetRating() float64          { return 0 }
func (p *Playlist) GetYear() int                { return 0 }
func (p *Playlist) GetAddedAt() int64           { return 0 }
func (p *Playlist) GetUpdatedAt() int64         { return p.UpdatedAt }
func (p *Playlist) GetItemType() string         { return "playlist" }
func (p *Playlist) GetWatchStatus() WatchStatus { return WatchStatusUnwatched }

func (p *Playlist) GetDescription() string {
	if p.ItemCount == 1 {
		return "1 item"
	}
	return fmt.Sprintf("%d items", p.ItemCount)
}

func (p *Playlist) CanDrillDown() bool { return true }

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
