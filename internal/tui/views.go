package tui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/lipgloss"
	"github.com/drake/goplex/internal/domain"
	"github.com/drake/goplex/internal/tui/styles"
)

// Pane represents a UI pane
type Pane int

const (
	PaneSidebar Pane = iota
	PaneBrowser
	PaneInspector
)

// BrowseLevel tracks where we are in hierarchical navigation
type BrowseLevel int

const (
	BrowseLevelLibrary BrowseLevel = iota // Movies or Shows list
	BrowseLevelShow                       // Seasons of a show
	BrowseLevelSeason                     // Episodes of a season
)

// NavContext tracks navigation breadcrumbs for TV shows
type NavContext struct {
	Level       BrowseLevel
	LibraryID   string
	LibraryName string
	ShowID      string
	ShowTitle   string
	SeasonID    string
	SeasonNum   int

	// State Restoration
	CursorPos  int
	PageOffset int
}

// Breadcrumb returns a breadcrumb string for the navigation context
func (n NavContext) Breadcrumb() string {
	parts := []string{n.LibraryName}

	if n.ShowTitle != "" {
		parts = append(parts, n.ShowTitle)
	}

	if n.SeasonNum > 0 {
		parts = append(parts, fmt.Sprintf("Season %d", n.SeasonNum))
	} else if n.SeasonID != "" && n.SeasonNum == 0 {
		parts = append(parts, "Specials")
	}

	return strings.Join(parts, " > ")
}

// RenderBreadcrumb renders the breadcrumb navigation
func RenderBreadcrumb(nav NavContext, width int) string {
	crumb := nav.Breadcrumb()
	if crumb == "" {
		crumb = " " // Ensure breadcrumb always takes up a line
	}
	if len(crumb) > width-4 {
		crumb = "..." + crumb[len(crumb)-width+7:]
	}
	// Pad to full width to ensure consistent line height
	padding := width - len(crumb)
	if padding > 0 {
		crumb = crumb + strings.Repeat(" ", padding)
	}
	return styles.AccentStyle.Render(crumb)
}

// RenderStatusBar renders the bottom status bar
func RenderStatusBar(status string, playerStatus string, width int) string {
	// Split available width between status and player info
	statusWidth := width - len(playerStatus) - 5
	if statusWidth < 0 {
		statusWidth = 0
	}

	left := styles.Truncate(status, statusWidth)
	right := playerStatus

	gap := width - len(left) - len(right) - 2
	if gap < 0 {
		gap = 0
	}

	return styles.StatusBarStyle.Width(width).Render(
		left + strings.Repeat(" ", gap) + right,
	)
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

// RenderHelp renders the help bar
func RenderHelp(keys []KeyHelp, width int) string {
	var parts []string
	for _, k := range keys {
		parts = append(parts, styles.HelpKeyStyle.Render(k.Key)+styles.HelpDescStyle.Render(" "+k.Desc))
	}

	help := strings.Join(parts, styles.HelpSepStyle.String())
	// Use lipgloss.Width for visible width (excludes ANSI codes)
	visibleWidth := lipgloss.Width(help)
	if visibleWidth > width {
		// Truncate to fit - rebuild without ANSI for accurate truncation
		var plain []string
		for _, k := range keys {
			plain = append(plain, k.Key+" "+k.Desc)
		}
		plainHelp := strings.Join(plain, " • ")
		if len(plainHelp) > width-3 {
			plainHelp = plainHelp[:width-3] + "..."
		}
		// Re-render truncated version (simplified - just dim the whole thing)
		return styles.DimStyle.Render(plainHelp)
	}
	// Pad to full width for consistent layout
	if visibleWidth < width {
		help += strings.Repeat(" ", width-visibleWidth)
	}
	return help
}

// KeyHelp represents a key binding help item
type KeyHelp struct {
	Key  string
	Desc string
}

// KeyHelpForPane returns context-sensitive key bindings based on focused pane
func KeyHelpForPane(pane Pane) []KeyHelp {
	common := []KeyHelp{
		{Key: "h/l", Desc: "pane"},
		{Key: "j/k", Desc: "nav"},
	}

	switch pane {
	case PaneSidebar:
		return append(common,
			KeyHelp{Key: "Enter", Desc: "select"},
			KeyHelp{Key: "s", Desc: "search"},
			KeyHelp{Key: "?", Desc: "help"},
		)
	case PaneBrowser:
		return append(common,
			KeyHelp{Key: "Enter", Desc: "play"},
			KeyHelp{Key: "w", Desc: "watched"},
			KeyHelp{Key: "u", Desc: "unwatched"},
			KeyHelp{Key: "/", Desc: "filter"},
			KeyHelp{Key: "?", Desc: "help"},
		)
	case PaneInspector:
		return append(common,
			KeyHelp{Key: "g/G", Desc: "top/bottom"},
			KeyHelp{Key: "?", Desc: "help"},
		)
	default:
		return common
	}
}

// DefaultKeyHelp returns the default key bindings (for backward compatibility)
func DefaultKeyHelp() []KeyHelp {
	return KeyHelpForPane(PaneBrowser)
}

// RenderMovieItem renders a movie item for the list
func RenderMovieItem(item domain.MediaItem, selected bool, width int) string {
	style := styles.NormalItemStyle
	if selected {
		style = styles.SelectedItemStyle
	}

	// Watch status indicator
	indicator := styles.RenderWatchStatus(item.IsPlayed, int64(item.ViewOffset.Milliseconds()))

	// Title with year
	title := item.Title
	if item.Year > 0 {
		title = fmt.Sprintf("%s (%d)", item.Title, item.Year)
	}
	title = styles.Truncate(title, width-10)

	// Format info
	format := ""
	if item.Format != "" {
		format = styles.DimBadgeStyle.Render(item.Format)
	}

	return style.Width(width).Render(
		fmt.Sprintf("%s %s %s", indicator, title, format),
	)
}

// RenderShowItem renders a TV show item for the list
func RenderShowItem(show domain.Show, selected bool, width int) string {
	style := styles.NormalItemStyle
	if selected {
		style = styles.SelectedItemStyle
	}

	// Watch status indicator
	var indicator string
	switch show.WatchStatus() {
	case domain.WatchStatusWatched:
		indicator = styles.PlayedCheck
	case domain.WatchStatusInProgress:
		indicator = styles.InProgressDot
	default:
		indicator = styles.UnplayedDot
	}

	// Title with year
	title := show.Title
	if show.Year > 0 {
		title = fmt.Sprintf("%s (%d)", show.Title, show.Year)
	}
	title = styles.Truncate(title, width-20)

	// Episode count badge
	badge := styles.DimBadgeStyle.Render(fmt.Sprintf("%d eps", show.EpisodeCount))

	return style.Width(width).Render(
		fmt.Sprintf("%s %s %s", indicator, title, badge),
	)
}

// RenderSeasonItem renders a season item for the list
func RenderSeasonItem(season domain.Season, selected bool, width int) string {
	style := styles.NormalItemStyle
	if selected {
		style = styles.SelectedItemStyle
	}

	// Watch status indicator
	var indicator string
	switch season.WatchStatus() {
	case domain.WatchStatusWatched:
		indicator = styles.PlayedCheck
	case domain.WatchStatusInProgress:
		indicator = styles.InProgressDot
	default:
		indicator = styles.UnplayedDot
	}

	// Title
	title := season.DisplayTitle()
	title = styles.Truncate(title, width-20)

	// Episode count and progress
	watched := season.EpisodeCount - season.UnwatchedCount
	progress := fmt.Sprintf("%d/%d", watched, season.EpisodeCount)
	badge := styles.DimBadgeStyle.Render(progress)

	return style.Width(width).Render(
		fmt.Sprintf("%s %s %s", indicator, title, badge),
	)
}

// RenderEpisodeItem renders an episode item for the list
func RenderEpisodeItem(item domain.MediaItem, selected bool, width int) string {
	style := styles.NormalItemStyle
	if selected {
		style = styles.SelectedItemStyle
	}

	// Watch status indicator
	indicator := styles.RenderWatchStatus(item.IsPlayed, int64(item.ViewOffset.Milliseconds()))

	// Episode code and title
	code := styles.AccentStyle.Render(item.EpisodeCode())
	title := styles.Truncate(item.Title, width-20)

	// Duration
	duration := styles.DimStyle.Render(item.FormattedDuration())

	return style.Width(width).Render(
		fmt.Sprintf("%s %s %s %s", indicator, code, title, duration),
	)
}

// RenderLibraryItem renders a library item for the sidebar
func RenderLibraryItem(lib domain.Library, selected bool, focused bool, width int) string {
	style := styles.NormalItemStyle
	if selected && focused {
		style = styles.FocusedItemStyle
	} else if selected {
		style = styles.SelectedItemStyle
	}

	icon := "📁"
	if lib.Type == "movie" {
		icon = "🎬"
	} else if lib.Type == "show" {
		icon = "📺"
	}

	title := styles.Truncate(lib.Name, width-4)
	return style.Width(width).Render(fmt.Sprintf("%s %s", icon, title))
}

// RenderInspector renders the inspector panel content
func RenderInspector(item interface{}, width int) string {
	switch v := item.(type) {
	case domain.MediaItem:
		return renderMediaItemInspector(v, width)
	case domain.Show:
		return renderShowInspector(v, width)
	case domain.Season:
		return renderSeasonInspector(v, width)
	default:
		return styles.DimStyle.Render("No item selected")
	}
}

func renderMediaItemInspector(item domain.MediaItem, width int) string {
	var b strings.Builder

	// Title
	title := item.Title
	if item.Type == domain.MediaTypeEpisode {
		title = fmt.Sprintf("%s - %s", item.EpisodeCode(), item.Title)
	}
	b.WriteString(styles.TitleStyle.Width(width).Render(title))
	b.WriteString("\n")

	// Show title for episodes
	if item.ShowTitle != "" {
		b.WriteString(styles.SubtitleStyle.Render(item.ShowTitle))
		b.WriteString("\n")
	}

	// Year
	if item.Year > 0 {
		b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Year: %d", item.Year)))
		b.WriteString("\n")
	}

	// Duration
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Duration: %s", item.FormattedDuration())))
	b.WriteString("\n")

	// Format
	if item.Format != "" {
		b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Format: %s", item.Format)))
		b.WriteString("\n")
	}

	// Watch status
	status := item.WatchStatus().String()
	if item.ViewOffset > 0 && !item.IsPlayed {
		progress := formatDuration(item.ViewOffset)
		status = fmt.Sprintf("%s at %s", status, progress)
	}
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Status: %s", status)))
	b.WriteString("\n\n")

	// Summary
	if item.Summary != "" {
		summary := wordWrap(item.Summary, width-2)
		b.WriteString(styles.SubtitleStyle.Render(summary))
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func renderShowInspector(show domain.Show, width int) string {
	var b strings.Builder

	// Title
	b.WriteString(styles.TitleStyle.Width(width).Render(show.Title))
	b.WriteString("\n")

	// Year
	if show.Year > 0 {
		b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Year: %d", show.Year)))
		b.WriteString("\n")
	}

	// Season/Episode counts
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Seasons: %d", show.SeasonCount)))
	b.WriteString("\n")
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Episodes: %d", show.EpisodeCount)))
	b.WriteString("\n")

	// Progress
	watched := show.EpisodeCount - show.UnwatchedCount
	progress := float64(watched) / float64(show.EpisodeCount) * 100
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Progress: %.0f%% (%d/%d)", progress, watched, show.EpisodeCount)))
	b.WriteString("\n\n")

	// Summary
	if show.Summary != "" {
		summary := wordWrap(show.Summary, width-2)
		b.WriteString(styles.SubtitleStyle.Render(summary))
	}

	return lipgloss.NewStyle().Width(width).Render(b.String())
}

func renderSeasonInspector(season domain.Season, width int) string {
	var b strings.Builder

	// Title
	b.WriteString(styles.TitleStyle.Width(width).Render(season.DisplayTitle()))
	b.WriteString("\n")

	// Show title
	b.WriteString(styles.SubtitleStyle.Render(season.ShowTitle))
	b.WriteString("\n\n")

	// Episode count
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Episodes: %d", season.EpisodeCount)))
	b.WriteString("\n")

	// Progress
	watched := season.EpisodeCount - season.UnwatchedCount
	progress := float64(watched) / float64(season.EpisodeCount) * 100
	b.WriteString(styles.DimStyle.Render(fmt.Sprintf("Progress: %.0f%% (%d/%d)", progress, watched, season.EpisodeCount)))
	b.WriteString("\n\n")

	// Progress bar
	b.WriteString(styles.RenderProgressBar(progress, width-4))

	return lipgloss.NewStyle().Width(width).Render(b.String())
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

// RenderSpinner renders a loading spinner
func RenderSpinner(frame int) string {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	return styles.SpinnerStyle.Render(frames[frame%len(frames)])
}

// RenderError renders an error message
func RenderError(err error, width int) string {
	msg := wordWrap(err.Error(), width-4)
	return styles.ErrorStyle.Render("Error: " + msg)
}

