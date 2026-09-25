package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/mmcdole/kino/internal/media"
)

// itemPresentation is the TUI's view of a media entity. Unsupported metadata
// has its natural zero value; entities need no placeholder display methods.
type itemPresentation struct {
	Title, SortTitle   string
	Year               int
	AddedAt, UpdatedAt int64
	Duration           time.Duration
	Rating             float64
	WatchStatus        media.WatchStatus
	DrillDown          bool
}

func present(item media.ListItem) itemPresentation {
	p := itemPresentation{Title: item.GetTitle()}
	switch v := item.(type) {
	case *media.Item:
		p.SortTitle, p.Year, p.AddedAt, p.UpdatedAt = v.SortTitle, v.Year, v.AddedAt, v.UpdatedAt
		p.Duration, p.Rating, p.WatchStatus = v.Duration, v.Rating, v.WatchStatus()
	case *media.Show:
		p.SortTitle, p.Year, p.AddedAt, p.UpdatedAt = v.SortTitle, v.Year, v.AddedAt, v.UpdatedAt
		p.Rating, p.WatchStatus, p.DrillDown = v.Rating, v.WatchStatus(), true
	case *media.Season:
		p.Title = seasonTitle(*v)
		p.WatchStatus, p.DrillDown = v.WatchStatus(), true
	case *media.Library:
		p.UpdatedAt, p.DrillDown = v.UpdatedAt, true
	case *media.Playlist:
		p.Duration, p.UpdatedAt, p.DrillDown = v.Duration, v.UpdatedAt, true
	}
	if p.SortTitle == "" {
		p.SortTitle = p.Title
	}
	return p
}

// runtimeLabel formats an item's runtime as "1h 42m" or "48m".
func runtimeLabel(m media.Item) string {
	h := int(m.Duration.Hours())
	mins := int(m.Duration.Minutes()) % 60
	if h > 0 {
		return fmt.Sprintf("%dh %dm", h, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// clockDuration formats a playback position as H:MM:SS or MM:SS.
func clockDuration(d time.Duration) string {
	totalSeconds := int64(d.Seconds())

	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
}

func episodeCode(m media.Item) string {
	if m.Type != media.TypeEpisode {
		return ""
	}
	return fmt.Sprintf("S%02dE%02d", m.SeasonNum, m.EpisodeNum)
}

func resolution(m media.Item) string {
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

func formattedFileSize(m media.Item) string {
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

func channelLayout(m media.Item) string {
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

func seasonTitle(s media.Season) string {
	if s.SeasonNum == 0 {
		return "Specials"
	}
	if s.Title != "" && s.Title != fmt.Sprintf("Season %d", s.SeasonNum) {
		return fmt.Sprintf("Season %d: %s", s.SeasonNum, s.Title)
	}
	return fmt.Sprintf("Season %d", s.SeasonNum)
}

// wordWrap wraps text to the specified width
func wordWrap(text string, width int) string {
	if width <= 0 {
		return text
	}

	var result strings.Builder
	words := strings.Fields(text)
	lineLen := 0

	for i, word := range words {
		wordLen := len(word)

		if lineLen+wordLen+1 > width && lineLen > 0 {
			result.WriteString("\n")
			lineLen = 0
		}

		if i > 0 && lineLen > 0 {
			result.WriteString(" ")
			lineLen++
		}

		result.WriteString(word)
		lineLen += wordLen
	}

	return result.String()
}
