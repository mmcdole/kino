package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// Layout constants for inspector
const (
	InspectorBorderHeight     = 2
	InspectorScrollIndicators = 2
)

// inspectorContent holds the three-zone layout content
type inspectorContent struct {
	header string // fixed top
	body   string // scrollable middle
	footer string // fixed bottom
}

// Inspector displays detailed metadata for the selected item
type Inspector struct {
	item          interface{}
	width         int
	height        int
	offset        int // scroll offset
	maxVisible    int // max visible lines
	libraryStates map[string]LibrarySyncState
}

// NewInspector creates a new inspector component
func NewInspector() Inspector {
	return Inspector{
		libraryStates: make(map[string]LibrarySyncState),
	}
}

// SetItem sets the item to display
func (i *Inspector) SetItem(item interface{}) {
	i.item = item
	i.offset = 0 // Reset scroll on item change
}

// SetLibraryStates sets the library sync states for displaying item counts
func (i *Inspector) SetLibraryStates(states map[string]LibrarySyncState) {
	i.libraryStates = states
}

// SetSize updates the component dimensions
func (i *Inspector) SetSize(width, height int) {
	i.width = width
	i.height = height
	// Calculate max visible lines (reserve space for border, scroll indicators, and title)
	i.maxVisible = height - InspectorBorderHeight - InspectorScrollIndicators - 2 // -1 for title, -1 for blank line
	if i.maxVisible < 1 {
		i.maxVisible = 1
	}
}

// HasItem returns true if there is an item to display
func (i Inspector) HasItem() bool {
	return i.item != nil
}

// Update handles messages (currently no-op, inspector is not focusable)
func (i Inspector) Update(_ tea.Msg) (Inspector, tea.Cmd) {
	return i, nil
}

// View renders the component
func (i Inspector) View() string {
	style := styles.InactiveBorder

	// Border takes 2 chars (1 each side), leave 1 char safety margin
	contentWidth := i.width - 3
	if contentWidth < 10 {
		contentWidth = 10
	}
	content := i.renderInspector(contentWidth)

	// Title line (styled, matching other columns)
	titleLine := styles.AccentStyle.Render(styles.Truncate("Info", contentWidth))

	// Three-zone layout: header is fixed, body scrolls, footer is fixed
	headerLines := splitLines(content.header)
	footerLines := splitLines(content.footer)
	bodyLines := splitLines(content.body)

	// Calculate available space for body
	availableForBody := i.maxVisible - len(headerLines) - len(footerLines)
	if availableForBody < 1 {
		availableForBody = 1
	}

	// Clamp body scroll offset
	totalBodyLines := len(bodyLines)
	maxOffset := totalBodyLines - availableForBody
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := i.offset
	if offset > maxOffset {
		offset = maxOffset
	}

	// Get visible body window
	end := offset + availableForBody
	if end > totalBodyLines {
		end = totalBodyLines
	}
	visibleBody := bodyLines[offset:end]

	// Scroll indicators for body only
	header := " "
	if offset > 0 {
		header = styles.DimStyle.Render("↑ more")
	}
	footer := " "
	if end < totalBodyLines {
		footer = styles.DimStyle.Render("↓ more")
	}

	// Assemble: title + header zone + scroll-up indicator + visible body + padding + scroll-down indicator + footer zone
	var parts []string
	parts = append(parts, titleLine)
	parts = append(parts, "")

	// Header zone (fixed)
	if len(headerLines) > 0 && content.header != "" {
		parts = append(parts, strings.Join(headerLines, "\n"))
	}

	// Scroll-up indicator
	parts = append(parts, header)

	// Visible body
	if len(visibleBody) > 0 {
		parts = append(parts, strings.Join(visibleBody, "\n"))
	}

	// Pad between body end and footer if body is shorter than available space
	visibleBodyCount := len(visibleBody)
	if visibleBodyCount < availableForBody {
		padding := availableForBody - visibleBodyCount
		for j := 0; j < padding; j++ {
			parts = append(parts, "")
		}
	}

	// Scroll-down indicator
	parts = append(parts, footer)

	// Footer zone (fixed, pinned to bottom)
	if len(footerLines) > 0 && content.footer != "" {
		parts = append(parts, strings.Join(footerLines, "\n"))
	}

	rendered := strings.Join(parts, "\n")

	// Subtract frame (border) size so total rendered size equals i.width x i.height
	frameW, frameH := style.GetFrameSize()

	return style.
		Width(i.width - frameW).
		Height(i.height - frameH).
		Render(rendered)
}

// renderInspector renders the inspector panel content as three zones
func (i Inspector) renderInspector(width int) inspectorContent {
	switch v := i.item.(type) {
	case *domain.MediaItem:
		return i.renderMediaItemInspector(*v, width)
	case *domain.Show:
		return i.renderShowInspector(*v, width)
	case *domain.Season:
		return inspectorContent{header: i.renderSeasonInspector(*v, width)}
	case *domain.Library:
		return inspectorContent{body: i.renderLibraryInspector(v, width)}
	case domain.Library:
		return inspectorContent{body: i.renderLibraryInspector(&v, width)}
	case *domain.Playlist:
		return inspectorContent{body: i.renderPlaylistInspector(*v, width)}
	default:
		return inspectorContent{body: styles.DimStyle.Render("No item selected")}
	}
}

func (i Inspector) renderMediaItemInspector(item domain.MediaItem, width int) inspectorContent {
	headerStr := renderMediaHeader(item, width)
	bodyStr := renderMediaBody(item, width)
	footerStr := renderMediaFooter(item, width)
	return inspectorContent{
		header: headerStr,
		body:   bodyStr,
		footer: footerStr,
	}
}

func renderMediaHeader(item domain.MediaItem, width int) string {
	var b strings.Builder

	// Title
	title := item.Title
	if item.Type == domain.MediaTypeEpisode {
		title = fmt.Sprintf("%s - %s", item.EpisodeCode(), item.Title)
	}
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(title, width)))
	b.WriteString("\n")

	// Show title for episodes
	if item.ShowTitle != "" {
		b.WriteString(styles.SubtitleStyle.Render(styles.Truncate(item.ShowTitle, width)))
		b.WriteString("\n")
	}

	// Meta line: Year • Duration • Content Rating
	var metaParts []string
	if item.Year > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%d", item.Year))
	}
	metaParts = append(metaParts, item.FormattedDuration())
	if item.ContentRating != "" {
		metaParts = append(metaParts, item.ContentRating)
	}
	b.WriteString(styles.DimStyle.Render(strings.Join(metaParts, " · ")))
	b.WriteString("\n")

	// Rating and watch status grouped left
	var statusParts []string
	if item.Rating > 0 {
		ratingText := fmt.Sprintf("★ %.1f", item.Rating)
		var ratingStyle lipgloss.Style
		switch {
		case item.Rating >= 7:
			ratingStyle = lipgloss.NewStyle().Foreground(styles.Green)
		case item.Rating >= 5:
			ratingStyle = lipgloss.NewStyle().Foreground(styles.PlexOrange)
		default:
			ratingStyle = lipgloss.NewStyle().Foreground(styles.Red)
		}
		statusParts = append(statusParts, ratingStyle.Render(ratingText))
	}

	switch item.WatchStatus() {
	case domain.WatchStatusWatched:
		statusParts = append(statusParts, styles.PlayedStyle.Render("✓ Watched"))
	case domain.WatchStatusInProgress:
		statusParts = append(statusParts, styles.InProgressStyle.Render(fmt.Sprintf("◐ %s", formatDuration(item.ViewOffset))))
	case domain.WatchStatusUnwatched:
		statusParts = append(statusParts, styles.DimStyle.Render("○ Unwatched"))
	}

	if len(statusParts) > 0 {
		b.WriteString(strings.Join(statusParts, "   "))
	}

	return strings.TrimRight(b.String(), "\n")
}

func renderMediaBody(item domain.MediaItem, width int) string {
	if item.Summary == "" {
		return ""
	}
	bodyWidth := width - 2
	if bodyWidth > 80 {
		bodyWidth = 80
	}
	summary := wordWrap(item.Summary, bodyWidth)
	return styles.SubtitleStyle.Render(summary)
}

func renderMediaFooter(item domain.MediaItem, width int) string {
	hasTech := item.VideoCodec != "" || item.AudioCodec != "" ||
		item.Container != "" || item.FileSize > 0

	if !hasTech {
		return ""
	}

	var b strings.Builder

	// Separator
	separator := strings.Repeat("─", width)
	b.WriteString(styles.DimStyle.Render(separator))
	b.WriteString("\n")

	// Row 1: container | video codec | resolution
	row1c1 := ""
	if item.Container != "" {
		row1c1 = strings.ToUpper(item.Container)
	}
	row1c2 := item.VideoCodec
	row1c3 := item.Resolution()

	// Row 2: audio codec | channel layout | filesize (or bitrate)
	row2c1 := item.AudioCodec
	row2c2 := item.ChannelLayout()
	row2c3 := ""
	if fs := item.FormattedFileSize(); fs != "" {
		row2c3 = fs
	} else if item.Bitrate >= 1000 {
		row2c3 = fmt.Sprintf("%.1f Mbps", float64(item.Bitrate)/1000)
	}

	// Calculate column widths from content across both rows
	col1W := len(row1c1)
	if len(row2c1) > col1W {
		col1W = len(row2c1)
	}
	col2W := len(row1c2)
	if len(row2c2) > col2W {
		col2W = len(row2c2)
	}

	// Dynamic gap: distribute leftover space, clamped to [1, 4]
	totalText := col1W + col2W + len(row1c3)
	if t2 := col1W + col2W + len(row2c3); t2 > totalText {
		totalText = t2
	}
	gap := (width - totalText - 2) / 2
	if gap < 1 {
		gap = 1
	}
	if gap > 4 {
		gap = 4
	}
	spacer := strings.Repeat(" ", gap)

	padTo := func(s string, w int) string {
		if len(s) >= w {
			return s
		}
		return s + strings.Repeat(" ", w-len(s))
	}

	b.WriteString(styles.DimStyle.Render(padTo(row1c1, col1W) + spacer + padTo(row1c2, col2W) + spacer + row1c3))
	b.WriteString("\n")
	b.WriteString(styles.DimStyle.Render(padTo(row2c1, col1W) + spacer + padTo(row2c2, col2W) + spacer + row2c3))

	return strings.TrimRight(b.String(), "\n")
}

func (i Inspector) renderShowInspector(show domain.Show, width int) inspectorContent {
	var header strings.Builder

	// Title
	header.WriteString(styles.TitleStyle.Render(styles.Truncate(show.Title, width)))
	header.WriteString("\n")

	// Meta line: Year • Content Rating
	var metaParts []string
	if show.Year > 0 {
		metaParts = append(metaParts, fmt.Sprintf("%d", show.Year))
	}
	if show.ContentRating != "" {
		metaParts = append(metaParts, show.ContentRating)
	}
	if len(metaParts) > 0 {
		header.WriteString(styles.DimStyle.Render(strings.Join(metaParts, " · ")))
		header.WriteString("\n")
	}

	// Rating
	if show.Rating > 0 {
		ratingText := fmt.Sprintf("★ %.1f", show.Rating)
		var ratingStyle lipgloss.Style
		switch {
		case show.Rating >= 7:
			ratingStyle = lipgloss.NewStyle().Foreground(styles.Green)
		case show.Rating >= 5:
			ratingStyle = lipgloss.NewStyle().Foreground(styles.PlexOrange)
		default:
			ratingStyle = lipgloss.NewStyle().Foreground(styles.Red)
		}
		header.WriteString(ratingStyle.Render(ratingText))
		header.WriteString("\n")
	}

	// Season/Episode counts and progress
	header.WriteString(styles.DimStyle.Render(fmt.Sprintf("Seasons: %d", show.SeasonCount)))
	header.WriteString("\n")
	header.WriteString(styles.DimStyle.Render(fmt.Sprintf("Episodes: %d", show.EpisodeCount)))
	header.WriteString("\n")

	watched := show.EpisodeCount - show.UnwatchedCount
	progress := float64(watched) / float64(show.EpisodeCount) * 100
	header.WriteString(styles.DimStyle.Render(fmt.Sprintf("Progress: %.0f%% (%d/%d)", progress, watched, show.EpisodeCount)))

	// Body: summary
	bodyStr := ""
	if show.Summary != "" {
		bodyWidth := width - 2
		if bodyWidth > 80 {
			bodyWidth = 80
		}
		bodyStr = styles.SubtitleStyle.Render(wordWrap(show.Summary, bodyWidth))
	}

	return inspectorContent{
		header: strings.TrimRight(header.String(), "\n"),
		body:   bodyStr,
	}
}

func (i Inspector) renderSeasonInspector(season domain.Season, width int) string {
	var b strings.Builder

	// Title
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(season.DisplayTitle(), width)))
	b.WriteString("\n")

	// Show title
	b.WriteString(styles.SubtitleStyle.Render(styles.Truncate(season.ShowTitle, width)))
	b.WriteString("\n")

	// Episode count
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Episodes: %d", season.EpisodeCount)))
	b.WriteString("\n")

	// Progress
	watched := season.EpisodeCount - season.UnwatchedCount
	progress := float64(watched) / float64(season.EpisodeCount) * 100
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Progress: %.0f%% (%d/%d)", progress, watched, season.EpisodeCount)))

	return b.String()
}

func (i Inspector) renderLibraryInspector(lib *domain.Library, width int) string {
	var b strings.Builder

	// Handle synthetic "Playlists" entry
	if lib.Type == "playlist" {
		b.WriteString(styles.TitleStyle.Render(styles.Truncate(lib.Name, width)))
		b.WriteString("\n\n")
		b.WriteString(styles.DimStyle.Render("Browse and manage your playlists"))
		b.WriteString("\n\n")
		b.WriteString(styles.SubtitleStyle.Render("Press Enter to browse"))
		return b.String()
	}

	// Library name as title
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(lib.Name, width)))
	b.WriteString("\n\n")

	// Library type
	typeLabel := "Movies"
	if lib.Type == "show" {
		typeLabel = "TV Shows"
	}
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Type: %s", typeLabel)))
	b.WriteString("\n")

	// Item count from sync state
	if state, ok := i.libraryStates[lib.ID]; ok && state.Loaded > 0 {
		b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Items: %d", state.Loaded)))
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.SubtitleStyle.Render("Press Enter to browse"))

	return b.String()
}

func (i Inspector) renderPlaylistInspector(playlist domain.Playlist, width int) string {
	var b strings.Builder

	// Playlist name as title
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(playlist.Title, width)))
	b.WriteString("\n\n")

	// Playlist type
	typeLabel := "Video"
	if playlist.PlaylistType == "audio" {
		typeLabel = "Audio"
	} else if playlist.PlaylistType == "photo" {
		typeLabel = "Photo"
	}
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Type: %s Playlist", typeLabel)))
	b.WriteString("\n")

	// Smart playlist indicator
	if playlist.Smart {
		b.WriteString(styles.DimStyle.Render("Smart: Yes"))
		b.WriteString("\n")
	}

	// Item count
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Items: %d", playlist.ItemCount)))
	b.WriteString("\n")

	// Duration
	if playlist.Duration > 0 {
		hours := int(playlist.Duration.Hours())
		minutes := int(playlist.Duration.Minutes()) % 60
		if hours > 0 {
			b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Duration: %dh %dm", hours, minutes)))
		} else {
			b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Duration: %dm", minutes)))
		}
		b.WriteString("\n")
	}

	b.WriteString("\n")
	b.WriteString(styles.SubtitleStyle.Render("Press Enter to browse"))
	b.WriteString("\n")
	b.WriteString(styles.DimStyle.Render("n: New Playlist"))
	b.WriteString("\n")
	b.WriteString(styles.DimStyle.Render("x: Delete Playlist"))

	return b.String()
}

// splitLines splits a string into lines, returning empty slice for empty string
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// formatDuration formats a duration as HH:MM:SS or MM:SS
func formatDuration(d interface{}) string {
	var totalSeconds int64

	switch v := d.(type) {
	case int64:
		totalSeconds = v / 1000 // assuming milliseconds
	case int:
		totalSeconds = int64(v) / 1000
	default:
		// Try to handle time.Duration
		if dur, ok := d.(interface{ Seconds() float64 }); ok {
			totalSeconds = int64(dur.Seconds())
		} else {
			return "00:00"
		}
	}

	hours := totalSeconds / 3600
	minutes := (totalSeconds % 3600) / 60
	seconds := totalSeconds % 60

	if hours > 0 {
		return fmt.Sprintf("%d:%02d:%02d", hours, minutes, seconds)
	}
	return fmt.Sprintf("%02d:%02d", minutes, seconds)
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
