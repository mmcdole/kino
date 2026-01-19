package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/drake/goplex/internal/domain"
	"github.com/drake/goplex/internal/tui/styles"
	"github.com/sahilm/fuzzy"
)


// ContentType represents the type of content being displayed
type ContentType int

const (
	ContentTypeMovies ContentType = iota
	ContentTypeShows
	ContentTypeSeasons
	ContentTypeEpisodes
	ContentTypeOnDeck
)

// Layout constants for grid
const (
	// Border adds 1 char on each side (left+right for width, top+bottom for height)
	BorderWidth  = 2
	BorderHeight = 2

	// Padding inside the border (Padding(0,1) = 1 left + 1 right)
	HorizontalPadding = 2

	// Scroll indicators ("↑ more" and "↓ more") each take 1 line
	ScrollIndicatorLines = 2

	// Breadcrumb line at top of content area
	BreadcrumbLines = 1

	// Extra safety margin for item width calculations
	ItemWidthMargin = 2
)

// Grid is the main content browser component
type Grid struct {
	// Content
	movies   []domain.MediaItem
	shows    []domain.Show
	seasons  []domain.Season
	episodes []domain.MediaItem

	contentType ContentType

	// Selection
	cursor     int
	offset     int
	maxVisible int

	// Dimensions
	width   int
	height  int
	focused bool

	// Border title (breadcrumb)
	breadcrumb string

	// Filter state
	filterActive  bool
	filterInput   textinput.Model
	filterQuery   string
	filteredIdx   []int // indices into original slice
}

// NewGrid creates a new grid component
func NewGrid() Grid {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.Prompt = "/ "
	ti.PromptStyle = styles.FilterPromptStyle
	ti.TextStyle = styles.FilterStyle

	return Grid{
		filterInput: ti,
	}
}

// SetMovies sets movies content
func (g *Grid) SetMovies(movies []domain.MediaItem) {
	g.movies = movies
	g.shows = nil
	g.seasons = nil
	g.episodes = nil
	g.contentType = ContentTypeMovies
	g.cursor = 0
	g.offset = 0
	g.clearFilter()
}

// SetShows sets TV shows content
func (g *Grid) SetShows(shows []domain.Show) {
	g.shows = shows
	g.movies = nil
	g.seasons = nil
	g.episodes = nil
	g.contentType = ContentTypeShows
	g.cursor = 0
	g.offset = 0
	g.clearFilter()
}

// SetSeasons sets seasons content
func (g *Grid) SetSeasons(seasons []domain.Season) {
	g.seasons = seasons
	g.movies = nil
	g.shows = nil
	g.episodes = nil
	g.contentType = ContentTypeSeasons
	g.cursor = 0
	g.offset = 0
	g.clearFilter()
}

// SetEpisodes sets episodes content
func (g *Grid) SetEpisodes(episodes []domain.MediaItem) {
	g.episodes = episodes
	g.movies = nil
	g.shows = nil
	g.seasons = nil
	g.contentType = ContentTypeEpisodes
	g.cursor = 0
	g.offset = 0
	g.clearFilter()
}

// SetOnDeck sets on deck content
func (g *Grid) SetOnDeck(items []domain.MediaItem) {
	g.movies = items
	g.shows = nil
	g.seasons = nil
	g.episodes = nil
	g.contentType = ContentTypeOnDeck
	g.cursor = 0
	g.offset = 0
	g.clearFilter()
}

// SetSize updates the component dimensions
func (g *Grid) SetSize(width, height int) {
	g.width = width
	g.height = height
	g.recalcMaxVisible()
}

// SetBreadcrumb sets the breadcrumb text displayed in the border title
func (g *Grid) SetBreadcrumb(crumb string) {
	g.breadcrumb = crumb
}

// recalcMaxVisible calculates maxVisible accounting for breadcrumb and filter bar
func (g *Grid) recalcMaxVisible() {
	// Interior height = total - border (top+bottom)
	// Reserve space for: breadcrumb + scroll indicators (header + footer)
	interiorHeight := g.height - BorderHeight
	g.maxVisible = interiorHeight - ScrollIndicatorLines - BreadcrumbLines
	// Reserve space for filter bar when active
	if g.filterActive {
		g.maxVisible--
	}
	if g.maxVisible < 1 {
		g.maxVisible = 1
	}
}

// SetFocused sets the focus state
func (g *Grid) SetFocused(focused bool) {
	g.focused = focused
}

// IsFocused returns the focus state
func (g Grid) IsFocused() bool {
	return g.focused
}

// ContentType returns the current content type
func (g Grid) ContentType() ContentType {
	return g.contentType
}

// Cursor returns the current cursor position
func (g Grid) Cursor() int {
	return g.cursor
}

// SetCursor sets the cursor position
func (g *Grid) SetCursor(pos int) {
	max := g.itemCount() - 1
	if max < 0 {
		g.cursor = 0
		return
	}
	if pos < 0 {
		pos = 0
	}
	if pos > max {
		pos = max
	}
	g.cursor = pos
	g.ensureVisible()
}

// itemCount returns the number of items (accounting for filter)
func (g Grid) itemCount() int {
	return g.filteredCount()
}

// SelectedItem returns the selected item
func (g Grid) SelectedItem() interface{} {
	count := g.itemCount()
	if count == 0 || g.cursor >= count {
		return nil
	}

	idx := g.mapIndex(g.cursor)
	switch g.contentType {
	case ContentTypeMovies, ContentTypeOnDeck:
		return g.movies[idx]
	case ContentTypeShows:
		return g.shows[idx]
	case ContentTypeSeasons:
		return g.seasons[idx]
	case ContentTypeEpisodes:
		return g.episodes[idx]
	default:
		return nil
	}
}

// SelectedMediaItem returns the selected item if it's a MediaItem
func (g Grid) SelectedMediaItem() *domain.MediaItem {
	switch g.contentType {
	case ContentTypeMovies, ContentTypeOnDeck, ContentTypeEpisodes:
		count := g.itemCount()
		if count == 0 || g.cursor >= count {
			return nil
		}
		idx := g.mapIndex(g.cursor)
		var items []domain.MediaItem
		if g.contentType == ContentTypeEpisodes {
			items = g.episodes
		} else {
			items = g.movies
		}
		return &items[idx]
	default:
		return nil
	}
}

// SelectedShow returns the selected show
func (g Grid) SelectedShow() *domain.Show {
	if g.contentType != ContentTypeShows {
		return nil
	}
	count := g.itemCount()
	if count == 0 || g.cursor >= count {
		return nil
	}
	idx := g.mapIndex(g.cursor)
	return &g.shows[idx]
}

// SelectedSeason returns the selected season
func (g Grid) SelectedSeason() *domain.Season {
	if g.contentType != ContentTypeSeasons {
		return nil
	}
	count := g.itemCount()
	if count == 0 || g.cursor >= count {
		return nil
	}
	idx := g.mapIndex(g.cursor)
	return &g.seasons[idx]
}

// ensureVisible ensures the cursor is visible
func (g *Grid) ensureVisible() {
	if g.cursor < g.offset {
		g.offset = g.cursor
	}
	if g.cursor >= g.offset+g.maxVisible {
		g.offset = g.cursor - g.maxVisible + 1
	}
}

// ToggleFilter activates the filter input
func (g *Grid) ToggleFilter() {
	g.filterActive = true
	g.filterInput.Focus()
	g.recalcMaxVisible()
}

// IsFiltering returns true if filter mode is active (showing filtered results)
func (g Grid) IsFiltering() bool {
	return g.filterActive
}

// IsFilterTyping returns true if filter is active AND input is focused (typing mode)
func (g Grid) IsFilterTyping() bool {
	return g.filterActive && g.filterInput.Focused()
}

// ClearFilter deactivates the filter and shows all items
func (g *Grid) ClearFilter() {
	g.clearFilter()
}

// clearFilter internal method to reset filter state
func (g *Grid) clearFilter() {
	g.filterActive = false
	g.filterQuery = ""
	g.filteredIdx = nil
	g.filterInput.SetValue("")
	g.filterInput.Blur()
	g.recalcMaxVisible()
}

// applyFilter filters items based on the current query
func (g *Grid) applyFilter() {
	query := g.filterInput.Value()
	g.filterQuery = query

	if query == "" {
		g.filteredIdx = nil
		return
	}

	// Get titles and do case-insensitive matching
	titles := g.getTitles()
	lowerTitles := make([]string, len(titles))
	for i, t := range titles {
		lowerTitles[i] = strings.ToLower(t)
	}

	matches := fuzzy.Find(strings.ToLower(query), lowerTitles)

	g.filteredIdx = make([]int, len(matches))
	for i, match := range matches {
		g.filteredIdx[i] = match.Index
	}

	// Reset cursor to first match
	g.cursor = 0
	g.offset = 0
}

// getTitles returns all titles for the current content type
func (g Grid) getTitles() []string {
	switch g.contentType {
	case ContentTypeMovies, ContentTypeOnDeck:
		titles := make([]string, len(g.movies))
		for i, m := range g.movies {
			titles[i] = m.Title
		}
		return titles
	case ContentTypeShows:
		titles := make([]string, len(g.shows))
		for i, s := range g.shows {
			titles[i] = s.Title
		}
		return titles
	case ContentTypeSeasons:
		titles := make([]string, len(g.seasons))
		for i, s := range g.seasons {
			titles[i] = s.DisplayTitle()
		}
		return titles
	case ContentTypeEpisodes:
		titles := make([]string, len(g.episodes))
		for i, e := range g.episodes {
			titles[i] = e.Title
		}
		return titles
	default:
		return nil
	}
}

// filteredCount returns the number of items after filtering
func (g Grid) filteredCount() int {
	if g.filteredIdx != nil {
		return len(g.filteredIdx)
	}
	return g.rawItemCount()
}

// rawItemCount returns the total number of items without filtering
func (g Grid) rawItemCount() int {
	switch g.contentType {
	case ContentTypeMovies, ContentTypeOnDeck:
		return len(g.movies)
	case ContentTypeShows:
		return len(g.shows)
	case ContentTypeSeasons:
		return len(g.seasons)
	case ContentTypeEpisodes:
		return len(g.episodes)
	default:
		return 0
	}
}

// mapIndex maps a cursor position to the actual index in the data
func (g Grid) mapIndex(i int) int {
	if g.filteredIdx != nil && i < len(g.filteredIdx) {
		return g.filteredIdx[i]
	}
	return i
}

// Init initializes the component
func (g Grid) Init() tea.Cmd {
	return nil
}

// Update handles messages
func (g Grid) Update(msg tea.Msg) (Grid, tea.Cmd) {
	if !g.focused {
		return g, nil
	}

	// Handle filter input when active AND focused (typing mode)
	if g.filterActive && g.filterInput.Focused() {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				g.clearFilter()
				return g, nil
			case "enter":
				// Accept filter, blur input to allow navigation
				g.filterInput.Blur()
				return g, nil
			case "backspace":
				if g.filterInput.Value() == "" {
					g.clearFilter()
					return g, nil
				}
			}
		}

		// Route to textinput
		var cmd tea.Cmd
		g.filterInput, cmd = g.filterInput.Update(msg)
		g.applyFilter()
		return g, cmd
	}

	// Handle keys when filter is active but blurred (navigation mode with filter results)
	if g.filterActive {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				// Clear filter and show all items
				g.clearFilter()
				return g, nil
			case "/":
				// Re-activate filter input
				g.filterInput.Focus()
				return g, nil
			}
		}
		// Fall through to normal navigation handling
	}

	count := g.itemCount()
	if count == 0 {
		return g, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if g.cursor < count-1 {
				g.cursor++
				g.ensureVisible()
			}
		case "k", "up":
			if g.cursor > 0 {
				g.cursor--
				g.ensureVisible()
			}
		case "g":
			g.cursor = 0
			g.offset = 0
		case "G":
			g.cursor = count - 1
			g.ensureVisible()
		case "ctrl+d":
			// Page down
			g.cursor += g.maxVisible / 2
			if g.cursor >= count {
				g.cursor = count - 1
			}
			g.ensureVisible()
		case "ctrl+u":
			// Page up
			g.cursor -= g.maxVisible / 2
			if g.cursor < 0 {
				g.cursor = 0
			}
			g.ensureVisible()
		}
	}

	return g, nil
}

// View renders the component
func (g Grid) View() string {
	style := styles.InactiveBorder
	if g.focused {
		style = styles.ActiveBorder
	}

	content := g.renderList()

	// Subtract frame (border) size so total rendered size equals g.width x g.height
	frameW, frameH := style.GetFrameSize()

	return style.
		Width(g.width - frameW).
		Height(g.height - frameH).
		Render(content)
}


// renderList renders the list view
func (g Grid) renderList() string {
	// Item width = total - border - padding - margin
	itemWidth := g.width - BorderWidth - HorizontalPadding - ItemWidthMargin

	// Breadcrumb is always first line (even if empty, for consistent layout)
	breadcrumbLine := " "
	if g.breadcrumb != "" {
		crumb := g.breadcrumb
		if len(crumb) > itemWidth {
			crumb = "..." + crumb[len(crumb)-itemWidth+3:]
		}
		breadcrumbLine = styles.AccentStyle.Render(crumb)
	}

	count := g.itemCount()
	if count == 0 {
		emptyMsg := styles.DimStyle.Render("No items")
		if g.filterActive && g.filterQuery != "" {
			emptyMsg = styles.DimStyle.Render("No matches")
		}
		return breadcrumbLine + "\n" + " " + "\n" + emptyMsg + "\n" + " "
	}

	var lines []string

	end := g.offset + g.maxVisible
	if end > count {
		end = count
	}

	for i := g.offset; i < end; i++ {
		selected := i == g.cursor
		idx := g.mapIndex(i)
		var line string

		switch g.contentType {
		case ContentTypeMovies, ContentTypeOnDeck:
			line = g.renderMovieItem(g.movies[idx], selected, itemWidth)
		case ContentTypeShows:
			line = g.renderShowItem(g.shows[idx], selected, itemWidth)
		case ContentTypeSeasons:
			line = g.renderSeasonItem(g.seasons[idx], selected, itemWidth)
		case ContentTypeEpisodes:
			line = g.renderEpisodeItem(g.episodes[idx], selected, itemWidth)
		}

		lines = append(lines, line)
	}

	// ALWAYS reserve space for header (even if empty) to prevent layout shifts
	header := " " // Empty placeholder when not scrolled
	if g.offset > 0 {
		header = styles.DimStyle.Render("↑ more")
	}

	// ALWAYS reserve space for footer (even if empty)
	footer := " " // Empty placeholder when at bottom
	if end < count {
		footer = styles.DimStyle.Render("↓ more")
	}

	content := strings.Join(lines, "\n")
	// Structure: breadcrumb, header scroll indicator, content, footer scroll indicator
	content = breadcrumbLine + "\n" + header + "\n" + content + "\n" + footer

	// Add filter bar at bottom if active
	if g.filterActive {
		content += "\n" + g.renderFilterBar(itemWidth)
	}

	return content
}

// renderMovieItem renders a movie item for the list
func (g Grid) renderMovieItem(item domain.MediaItem, selected bool, width int) string {
	// Determine indicator character and color
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if item.IsPlayed {
		indicatorChar = styles.PlayedChar
		indicatorFg = styles.Green
	} else if item.ViewOffset.Milliseconds() > 0 {
		indicatorChar = styles.InProgressChar
		indicatorFg = styles.PlexOrange
	} else {
		indicatorChar = styles.UnplayedChar
		indicatorFg = styles.PlexOrange
	}

	title := item.Title
	if item.Year > 0 {
		title = fmt.Sprintf("%s (%d)", item.Title, item.Year)
	}
	title = styles.Truncate(title, width-10)

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
	}

	return styles.RenderListRow(parts, selected, width)
}

// renderShowItem renders a TV show item for the list
func (g Grid) renderShowItem(show domain.Show, selected bool, width int) string {
	// Determine indicator character and color
	var indicatorChar string
	var indicatorFg lipgloss.Color
	switch show.WatchStatus() {
	case domain.WatchStatusWatched:
		indicatorChar = styles.PlayedChar
		indicatorFg = styles.Green
	case domain.WatchStatusInProgress:
		indicatorChar = styles.InProgressChar
		indicatorFg = styles.PlexOrange
	default:
		indicatorChar = styles.UnplayedChar
		indicatorFg = styles.PlexOrange
	}

	title := show.Title
	if show.Year > 0 {
		title = fmt.Sprintf("%s (%d)", show.Title, show.Year)
	}
	title = styles.Truncate(title, width-20)
	badge := fmt.Sprintf(" %d eps", show.EpisodeCount)
	dimGray := styles.DimGray

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
		{Text: badge, Foreground: &dimGray},
	}

	return styles.RenderListRow(parts, selected, width)
}

// renderSeasonItem renders a season item for the list
func (g Grid) renderSeasonItem(season domain.Season, selected bool, width int) string {
	// Determine indicator character and color
	var indicatorChar string
	var indicatorFg lipgloss.Color
	switch season.WatchStatus() {
	case domain.WatchStatusWatched:
		indicatorChar = styles.PlayedChar
		indicatorFg = styles.Green
	case domain.WatchStatusInProgress:
		indicatorChar = styles.InProgressChar
		indicatorFg = styles.PlexOrange
	default:
		indicatorChar = styles.UnplayedChar
		indicatorFg = styles.PlexOrange
	}

	title := season.DisplayTitle()
	title = styles.Truncate(title, width-20)
	watched := season.EpisodeCount - season.UnwatchedCount
	badge := fmt.Sprintf(" %d/%d", watched, season.EpisodeCount)
	dimGray := styles.DimGray

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + title, Foreground: nil},
		{Text: badge, Foreground: &dimGray},
	}

	return styles.RenderListRow(parts, selected, width)
}

// renderEpisodeItem renders an episode item for the list
func (g Grid) renderEpisodeItem(item domain.MediaItem, selected bool, width int) string {
	// Determine indicator character and color
	var indicatorChar string
	var indicatorFg lipgloss.Color
	if item.IsPlayed {
		indicatorChar = styles.PlayedChar
		indicatorFg = styles.Green
	} else if item.ViewOffset.Milliseconds() > 0 {
		indicatorChar = styles.InProgressChar
		indicatorFg = styles.PlexOrange
	} else {
		indicatorChar = styles.UnplayedChar
		indicatorFg = styles.PlexOrange
	}

	code := item.EpisodeCode()
	title := styles.Truncate(item.Title, width-20)
	duration := " " + item.FormattedDuration()
	plexOrange := styles.PlexOrange
	dimGray := styles.DimGray

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + code, Foreground: &plexOrange},
		{Text: " " + title, Foreground: nil},
		{Text: duration, Foreground: &dimGray},
	}

	return styles.RenderListRow(parts, selected, width)
}

// IsEmpty returns true if there are no items
func (g Grid) IsEmpty() bool {
	return g.itemCount() == 0
}

// renderFilterBar renders the filter input bar
func (g Grid) renderFilterBar(width int) string {
	input := g.filterInput.View()
	count := g.itemCount()
	total := g.rawItemCount()

	// Show match count
	countStr := ""
	if g.filterQuery != "" {
		countStr = styles.DimStyle.Render(fmt.Sprintf(" [%d/%d]", count, total))
	}

	return input + countStr
}
