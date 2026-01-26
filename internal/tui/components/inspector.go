package components

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// Layout constants for inspector
const (
	InspectorBorderHeight    = 2
	InspectorScrollIndicators = 2
)

// Inspector displays detailed metadata for the selected item
type Inspector struct {
	item          interface{}
	width         int
	height        int
	focused       bool
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
	i.maxVisible = height - InspectorBorderHeight - InspectorScrollIndicators - 1 // -1 for title
	if i.maxVisible < 1 {
		i.maxVisible = 1
	}
}

// SetFocused sets the focus state
func (i *Inspector) SetFocused(focused bool) {
	i.focused = focused
}

// IsFocused returns the focus state
func (i Inspector) IsFocused() bool {
	return i.focused
}

// Clear clears the inspector
func (i *Inspector) Clear() {
	i.item = nil
}

// HasItem returns true if there is an item to display
func (i Inspector) HasItem() bool {
	return i.item != nil
}

// Init initializes the component
func (i Inspector) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (i Inspector) Update(msg tea.Msg) (Inspector, tea.Cmd) {
	if !i.focused {
		return i, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			i.offset++
		case "k", "up":
			if i.offset > 0 {
				i.offset--
			}
		case "g":
			i.offset = 0
		case "G":
			i.offset = i.contentLineCount() // Will be clamped in View
		case "ctrl+d":
			i.offset += i.maxVisible / 2
		case "ctrl+u":
			i.offset -= i.maxVisible / 2
			if i.offset < 0 {
				i.offset = 0
			}
		}
	}

	return i, nil
}

// contentLineCount returns the total number of lines in the current content
func (i Inspector) contentLineCount() int {
	contentWidth := i.width - 3
	if contentWidth < 10 {
		contentWidth = 10
	}
	content := i.renderInspector(contentWidth)
	return len(strings.Split(content, "\n"))
}

// View renders the component
func (i Inspector) View() string {
	style := styles.InactiveBorder
	if i.focused {
		style = styles.ActiveBorder
	}

	// Border takes 2 chars (1 each side), leave 1 char safety margin
	contentWidth := i.width - 3
	if contentWidth < 10 {
		contentWidth = 10
	}
	fullContent := i.renderInspector(contentWidth)

	// Split into lines for scrolling
	lines := strings.Split(fullContent, "\n")
	totalLines := len(lines)

	// Clamp offset
	maxOffset := totalLines - i.maxVisible
	if maxOffset < 0 {
		maxOffset = 0
	}
	offset := i.offset
	if offset > maxOffset {
		offset = maxOffset
	}

	// Get visible window of lines
	end := offset + i.maxVisible
	if end > totalLines {
		end = totalLines
	}

	visibleLines := lines[offset:end]

	// ALWAYS reserve space for header (even if empty) to prevent layout shifts
	header := " "
	if offset > 0 {
		header = styles.DimStyle.Render("↑ more")
	}

	// ALWAYS reserve space for footer (even if empty)
	footer := " "
	if end < totalLines {
		footer = styles.DimStyle.Render("↓ more")
	}

	// Title line (styled, matching other columns)
	titleLine := styles.AccentStyle.Render(styles.Truncate("Info", contentWidth))

	content := titleLine + "\n" + header + "\n" + strings.Join(visibleLines, "\n") + "\n" + footer

	// Subtract frame (border) size so total rendered size equals i.width x i.height
	frameW, frameH := style.GetFrameSize()

	return style.
		Width(i.width - frameW).
		Height(i.height - frameH).
		Render(content)
}

// renderInspector renders the inspector panel content
func (i Inspector) renderInspector(width int) string {
	switch v := i.item.(type) {
	case *domain.MediaItem:
		return i.renderMediaItemInspector(*v, width)
	case domain.MediaItem:
		return i.renderMediaItemInspector(v, width)
	case *domain.Show:
		return i.renderShowInspector(*v, width)
	case domain.Show:
		return i.renderShowInspector(v, width)
	case *domain.Season:
		return i.renderSeasonInspector(*v, width)
	case domain.Season:
		return i.renderSeasonInspector(v, width)
	case *domain.Library:
		return i.renderLibraryInspector(*v, width)
	case domain.Library:
		return i.renderLibraryInspector(v, width)
	case *domain.Playlist:
		return i.renderPlaylistInspector(*v, width)
	case domain.Playlist:
		return i.renderPlaylistInspector(v, width)
	default:
		return styles.DimStyle.Render("No item selected")
	}
}

func (i Inspector) renderMediaItemInspector(item domain.MediaItem, width int) string {
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
		summary := wordWrap(item.Summary, width)
		b.WriteString(styles.SubtitleStyle.Render(summary))
	}

	return b.String()
}

func (i Inspector) renderShowInspector(show domain.Show, width int) string {
	var b strings.Builder

	// Title
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(show.Title, width)))
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
		summary := wordWrap(show.Summary, width)
		b.WriteString(styles.SubtitleStyle.Render(summary))
	}

	return b.String()
}

func (i Inspector) renderSeasonInspector(season domain.Season, width int) string {
	var b strings.Builder

	// Title
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(season.DisplayTitle(), width)))
	b.WriteString("\n")

	// Show title
	b.WriteString(styles.SubtitleStyle.Render(styles.Truncate(season.ShowTitle, width)))
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
	b.WriteString(styles.RenderProgressBar(progress, width))

	return b.String()
}

func (i Inspector) renderLibraryInspector(lib domain.Library, width int) string {
	var b strings.Builder

	// Handle synthetic "Playlists" entry
	if lib.Type == "playlist" {
		b.WriteString(styles.TitleStyle.Render(styles.Truncate(lib.Name, width)))
		b.WriteString("\n\n")
		b.WriteString(styles.DimStyle.Render("Browse and manage your playlists"))
		b.WriteString("\n\n")
		b.WriteString(styles.SubtitleStyle.Render("Press Enter to browse"))
		b.WriteString("\n")
		b.WriteString(styles.DimStyle.Render("P: Jump here from anywhere"))
		return b.String()
	}

	// Library name as title
	b.WriteString(styles.TitleStyle.Render(styles.Truncate(lib.Name, width)))
	b.WriteString("\n\n")

	// Library type
	typeLabel := "Movies"
	if lib.IsShowLibrary() {
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
