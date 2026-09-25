package components

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// renderRow renders one item. The row's layout follows the item's type.
func (c *ListColumn) renderRow(item domain.ListItem, selected bool, width int) string {
	switch v := item.(type) {
	case *domain.Library:
		return c.renderLibraryItem(*v, selected, width)
	case *domain.Show:
		return c.renderShowItem(*v, selected, width)
	case *domain.Season:
		return c.renderSeasonItem(*v, selected, width)
	case *domain.Playlist:
		return c.renderPlaylistItem(*v, selected, width)
	case *domain.MediaItem:
		switch {
		case v.Type != domain.MediaTypeEpisode:
			return c.renderMovieItem(*v, selected, width)
		case c.opts.ShowParent:
			return c.renderEpisodeWithShow(*v, selected, width)
		default:
			return c.renderEpisodeItem(*v, selected, width)
		}
	}
	return ""
}

func (c *ListColumn) renderLibraryItem(lib domain.Library, selected bool, width int) string {
	// Get the summary and activity for this library (works for playlists too via playlistsLibraryID)
	state := c.libraryStates[lib.ID]

	var prefix string
	var prefixFg lipgloss.Color

	switch {
	case state.Activity.Visible:
		prefix = styles.SpinnerFrames[c.spinnerFrame%len(styles.SpinnerFrames)] + " "
		prefixFg = styles.PlexOrange
	case state.Error != nil:
		prefix = "✗ "
		prefixFg = styles.Red
	default:
		prefix = "  "
		prefixFg = styles.DimGray
	}

	title := lib.Name
	if c.showLibraryCounts && state.Summary.Known {
		title = fmt.Sprintf("%s (%d)", lib.Name, state.Summary.Count)
	}
	title = styles.Truncate(title, width-4)

	parts := []styles.RowPart{
		{Text: prefix, Foreground: &prefixFg},
		{Text: title, Foreground: nil},
	}

	return styles.RenderListRow(parts, selected, width)
}

func (c *ListColumn) renderMovieItem(item domain.MediaItem, selected bool, width int) string {
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if c.showWatchStatus {
		indicatorChar, indicatorFg = mediaItemWatchIndicator(item)
	} else {
		indicatorChar = " "
	}

	title := item.Title
	if item.Year > 0 {
		title = fmt.Sprintf("%s (%d)", item.Title, item.Year)
	}

	// Available space: width - indicator(1) - space(1) - margins(2)
	availableForTitle := width - 4
	tag := c.sortTag(&item)
	if tag != "" {
		availableForTitle -= len(tag) + 1
	}
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title = styles.Truncate(title, availableForTitle)

	parts := appendSortTag([]styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
	}, tag, width)

	return styles.RenderListRow(parts, selected, width)
}

func (c *ListColumn) renderShowItem(show domain.Show, selected bool, width int) string {
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if c.showWatchStatus {
		indicatorChar, indicatorFg = watchIndicator(show.WatchStatus())
	} else {
		indicatorChar = " "
	}

	title := show.Title
	if show.Year > 0 {
		title = fmt.Sprintf("%s (%d)", show.Title, show.Year)
	}

	// Available space: width - indicator(1) - space(1) - margins(2)
	availableForTitle := width - 4
	tag := c.sortTag(&show)
	if tag != "" {
		availableForTitle -= len(tag) + 1
	}
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title = styles.Truncate(title, availableForTitle)

	parts := appendSortTag([]styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
	}, tag, width)

	return styles.RenderListRow(parts, selected, width)
}

func (c *ListColumn) renderSeasonItem(season domain.Season, selected bool, width int) string {
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if c.showWatchStatus {
		indicatorChar, indicatorFg = watchIndicator(season.WatchStatus())
	} else {
		indicatorChar = " "
	}

	title := seasonTitle(season)

	// Available space: width - indicator(1) - space(1) - margins(2)
	availableForTitle := width - 4
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title = styles.Truncate(title, availableForTitle)

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
	}

	return styles.RenderListRow(parts, selected, width)
}

func (c *ListColumn) renderEpisodeItem(item domain.MediaItem, selected bool, width int) string {
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if c.showWatchStatus {
		indicatorChar, indicatorFg = mediaItemWatchIndicator(item)
	} else {
		indicatorChar = " "
	}

	code := episodeCode(item)
	plexOrange := styles.PlexOrange

	// Available space: width - indicator(1) - space(1) - code - space(1) - margins(2)
	availableForTitle := width - 4 - len(code) - 1
	tag := c.sortTag(&item)
	if tag != "" {
		availableForTitle -= len(tag) + 1
	}
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title := styles.Truncate(item.Title, availableForTitle)

	parts := appendSortTag([]styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + code, Foreground: &plexOrange},
		{Text: " " + title, Foreground: nil},
	}, tag, width)

	return styles.RenderListRow(parts, selected, width)
}

func (c *ListColumn) renderPlaylistItem(playlist domain.Playlist, selected bool, width int) string {
	// Playlist icon and count
	prefix := "▶ "
	prefixFg := styles.PlexOrange

	title := playlist.Title
	countStr := fmt.Sprintf(" (%d)", playlist.ItemCount)

	// Available space: width - prefix(2) - count - margins(2)
	availableForTitle := width - 4 - len(countStr)
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title = styles.Truncate(title, availableForTitle)

	dimGray := styles.DimGray
	parts := []styles.RowPart{
		{Text: prefix, Foreground: &prefixFg},
		{Text: title, Foreground: nil},
		{Text: countStr, Foreground: &dimGray},
	}

	return styles.RenderListRow(parts, selected, width)
}

// renderEpisodeWithShow names the episode's show, for lists that mix shows.
func (c *ListColumn) renderEpisodeWithShow(item domain.MediaItem, selected bool, width int) string {
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if c.showWatchStatus {
		indicatorChar, indicatorFg = mediaItemWatchIndicator(item)
	} else {
		indicatorChar = " "
	}

	title := item.Title
	if item.Type == domain.MediaTypeEpisode && item.ShowTitle != "" {
		// Show episode with show context: "Show - S01E05 Title"
		title = fmt.Sprintf("%s - %s %s", item.ShowTitle, episodeCode(item), item.Title)
	} else if item.Year > 0 {
		title = fmt.Sprintf("%s (%d)", item.Title, item.Year)
	}

	// Available space: width - indicator(1) - space(1) - margins(2)
	availableForTitle := width - 4
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title = styles.Truncate(title, availableForTitle)

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
	}

	return styles.RenderListRow(parts, selected, width)
}

// watchIndicator returns the character and color for a watch status
func watchIndicator(status domain.WatchStatus) (string, lipgloss.Color) {
	switch status {
	case domain.WatchStatusWatched:
		return styles.PlayedChar, styles.Green
	case domain.WatchStatusInProgress:
		return styles.InProgressChar, styles.PlexOrange
	default:
		return styles.UnplayedChar, styles.PlexOrange
	}
}

// mediaItemWatchIndicator returns the character and color for a media item's watch status
func mediaItemWatchIndicator(item domain.MediaItem) (string, lipgloss.Color) {
	if item.IsPlayed {
		return styles.PlayedChar, styles.Green
	}
	if item.ViewOffset.Milliseconds() > 0 {
		return styles.InProgressChar, styles.PlexOrange
	}
	return styles.UnplayedChar, styles.PlexOrange
}

// sortTag returns a right-aligned tag string for the current sort field, or "" if
// sorting by the default field or the value is zero/empty.
func (c *ListColumn) sortTag(item domain.ListItem) string {
	if c.view.sortField == SortTitle || c.view.sortField == SortEpisodeNum {
		return ""
	}

	switch c.view.sortField {
	case SortDuration:
		d := present(item).Duration
		if d <= 0 {
			return ""
		}
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if h > 0 {
			return fmt.Sprintf("%dh %dm", h, m)
		}
		return fmt.Sprintf("%dm", m)
	case SortRating:
		r := present(item).Rating
		if r == 0 {
			return ""
		}
		return fmt.Sprintf("%.1f", r)
	case SortDateAdded, SortLastUpdated:
		var ts int64
		if c.view.sortField == SortDateAdded {
			ts = present(item).AddedAt
		} else {
			ts = present(item).UpdatedAt
		}
		if ts == 0 {
			return ""
		}
		return formatMonthYear(ts)
	case SortReleased:
		// Movie and show rows already include the year in the title
		if m, ok := item.(*domain.MediaItem); !ok || m.Type != domain.MediaTypeEpisode {
			return ""
		}
		y := present(item).Year
		if y == 0 {
			return ""
		}
		return fmt.Sprintf("%d", y)
	}
	return ""
}

// appendSortTag appends a right-aligned dim gray tag to the row parts.
// It calculates the gap needed to push the tag to the right edge within the given width.
func appendSortTag(parts []styles.RowPart, tag string, width int) []styles.RowPart {
	if tag == "" {
		return parts
	}
	used := 2 // left + right margin
	for _, p := range parts {
		used += lipgloss.Width(p.Text)
	}
	gap := width - used - len(tag)
	if gap < 1 {
		gap = 1
	}
	dimGray := styles.DimGray
	return append(parts, styles.RowPart{Text: strings.Repeat(" ", gap) + tag, Foreground: &dimGray})
}

// formatMonthYear formats a unix timestamp as "Jan 2006"
func formatMonthYear(ts int64) string {
	return time.Unix(ts, 0).Format("Jan 2006")
}
