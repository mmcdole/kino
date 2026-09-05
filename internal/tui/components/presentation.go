package components

import (
	"fmt"
	"time"

	"github.com/mmcdole/kino/internal/domain"
)

// itemPresentation is the TUI's view of a domain entity. Unsupported metadata
// has its natural zero value; entities need no placeholder display methods.
type itemPresentation struct {
	Title, SortTitle   string
	Year               int
	AddedAt, UpdatedAt int64
	Duration           time.Duration
	Rating             float64
	WatchStatus        domain.WatchStatus
	DrillDown          bool
}

func present(item domain.ListItem) itemPresentation {
	p := itemPresentation{Title: item.GetTitle()}
	switch v := item.(type) {
	case *domain.MediaItem:
		p.SortTitle, p.Year, p.AddedAt, p.UpdatedAt = v.SortTitle, v.Year, v.AddedAt, v.UpdatedAt
		p.Duration, p.Rating, p.WatchStatus = v.Duration, v.Rating, v.WatchStatus()
	case *domain.Show:
		p.SortTitle, p.Year, p.AddedAt, p.UpdatedAt = v.SortTitle, v.Year, v.AddedAt, v.UpdatedAt
		p.Rating, p.WatchStatus, p.DrillDown = v.Rating, v.WatchStatus(), true
	case *domain.Season:
		p.Title = seasonTitle(*v)
		p.WatchStatus, p.DrillDown = v.WatchStatus(), true
	case *domain.Library:
		p.UpdatedAt, p.DrillDown = v.UpdatedAt, true
	case *domain.Playlist:
		p.Duration, p.UpdatedAt, p.DrillDown = v.Duration, v.UpdatedAt, true
	}
	if p.SortTitle == "" {
		p.SortTitle = p.Title
	}
	return p
}

func formattedDuration(m domain.MediaItem) string {
	h := int(m.Duration.Hours())
	mins := int(m.Duration.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

func episodeCode(m domain.MediaItem) string {
	if m.Type != domain.MediaTypeEpisode {
		return ""
	}
	return fmt.Sprintf("S%02dE%02d", m.SeasonNum, m.EpisodeNum)
}

func resolution(m domain.MediaItem) string {
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

func formattedFileSize(m domain.MediaItem) string {
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

func channelLayout(m domain.MediaItem) string {
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

func seasonTitle(s domain.Season) string {
	if s.SeasonNum == 0 {
		return "Specials"
	}
	if s.Title != "" && s.Title != fmt.Sprintf("Season %d", s.SeasonNum) {
		return fmt.Sprintf("Season %d: %s", s.SeasonNum, s.Title)
	}
	return fmt.Sprintf("Season %d", s.SeasonNum)
}
