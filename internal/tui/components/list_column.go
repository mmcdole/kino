package components

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/styles"
	"github.com/sahilm/fuzzy"
)

// Spinner frames for loading animation
var listColumnSpinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

// Layout constants for list columns
const (
	// Border adds 1 char on each side (left+right for width, top+bottom for height)
	BorderWidth  = 2
	BorderHeight = 2

	// Scroll indicators ("↑ more" and "↓ more") each take 1 line
	ScrollIndicatorLines = 2
)

// ListColumn is a scrollable list column that can display various content types.
// It implements the Column interface.
type ListColumn struct {
	// Content - only one of these is populated at a time (using pointers for memory efficiency)
	libraries []domain.Library
	movies    []*domain.MediaItem
	shows     []*domain.Show
	seasons   []*domain.Season
	episodes  []*domain.MediaItem

	columnType ColumnType

	// Selection
	cursor     int
	offset     int
	maxVisible int

	// Dimensions
	width   int
	height  int
	focused bool

	// Column title (shown in header)
	title string

	// Loading state
	loading      bool
	spinnerFrame int

	// Library sync states (for library column)
	libraryStates map[string]LibrarySyncState

	// Sort state
	sortField  SortField
	sortDir    SortDirection
	sortedIdx  []int // sorted position → raw index (nil = default order)

	// Filter state
	filterActive bool
	filterInput  textinput.Model
	filterQuery  string
	filteredIdx  []int // indices into sorted slice (or raw if no sort)
}

// NewListColumn creates a new list column with the given type and title
func NewListColumn(colType ColumnType, title string) *ListColumn {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.Prompt = "/ "
	ti.PromptStyle = styles.FilterPromptStyle
	ti.TextStyle = styles.FilterStyle

	return &ListColumn{
		columnType:    colType,
		title:         title,
		filterInput:   ti,
		libraryStates: make(map[string]LibrarySyncState),
	}
}

// NewLibraryColumn creates a column for displaying libraries
func NewLibraryColumn(libraries []domain.Library) *ListColumn {
	col := NewListColumn(ColumnTypeLibraries, "Libraries")
	col.libraries = libraries
	return col
}

// NewMoviesColumn creates a column for displaying movies
func NewMoviesColumn(title string, movies []*domain.MediaItem) *ListColumn {
	col := NewListColumn(ColumnTypeMovies, title)
	col.movies = movies
	return col
}

// NewShowsColumn creates a column for displaying TV shows
func NewShowsColumn(title string, shows []*domain.Show) *ListColumn {
	col := NewListColumn(ColumnTypeShows, title)
	col.shows = shows
	return col
}

// NewSeasonsColumn creates a column for displaying seasons
func NewSeasonsColumn(title string, seasons []*domain.Season) *ListColumn {
	col := NewListColumn(ColumnTypeSeasons, title)
	col.seasons = seasons
	return col
}

// NewEpisodesColumn creates a column for displaying episodes
func NewEpisodesColumn(title string, episodes []*domain.MediaItem) *ListColumn {
	col := NewListColumn(ColumnTypeEpisodes, title)
	col.episodes = episodes
	return col
}

// Implement Column interface

func (c *ListColumn) Init() tea.Cmd {
	return nil
}

func (c *ListColumn) Update(msg tea.Msg) (Column, tea.Cmd) {
	if !c.focused {
		return c, nil
	}

	// Handle filter input when active AND focused (typing mode)
	if c.filterActive && c.filterInput.Focused() {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				c.clearFilter()
				return c, nil
			case "enter":
				// Accept filter, blur input to allow navigation
				c.filterInput.Blur()
				return c, nil
			case "backspace":
				if c.filterInput.Value() == "" {
					c.clearFilter()
					return c, nil
				}
			}
		}

		// Route to textinput
		var cmd tea.Cmd
		c.filterInput, cmd = c.filterInput.Update(msg)
		c.applyFilter()
		return c, cmd
	}

	// Handle keys when filter is active but blurred (navigation mode with filter results)
	if c.filterActive {
		switch msg := msg.(type) {
		case tea.KeyMsg:
			switch msg.String() {
			case "esc":
				// Clear filter and show all items
				c.clearFilter()
				return c, nil
			case "/":
				// Re-activate filter input
				c.filterInput.Focus()
				return c, nil
			}
		}
		// Fall through to normal navigation handling
	}

	count := c.ItemCount()
	if count == 0 {
		return c, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "j", "down":
			if c.cursor < count-1 {
				c.cursor++
				c.ensureVisible()
			}
		case "k", "up":
			if c.cursor > 0 {
				c.cursor--
				c.ensureVisible()
			}
		case "g", "home":
			c.cursor = 0
			c.offset = 0
		case "G", "end":
			c.cursor = count - 1
			c.ensureVisible()
		case "ctrl+d":
			// Half page down
			c.cursor += c.maxVisible / 2
			if c.cursor >= count {
				c.cursor = count - 1
			}
			c.ensureVisible()
		case "ctrl+u":
			// Half page up
			c.cursor -= c.maxVisible / 2
			if c.cursor < 0 {
				c.cursor = 0
			}
			c.ensureVisible()
		case "pgdown":
			// Full page down
			c.cursor += c.maxVisible
			if c.cursor >= count {
				c.cursor = count - 1
			}
			c.ensureVisible()
		case "pgup":
			// Full page up
			c.cursor -= c.maxVisible
			if c.cursor < 0 {
				c.cursor = 0
			}
			c.ensureVisible()
		}
	}

	return c, nil
}

func (c *ListColumn) View() string {
	style := styles.InactiveBorder
	if c.focused {
		style = styles.ActiveBorder
	}

	content := c.renderContent()

	// Subtract frame (border) size so total rendered size equals c.width x c.height
	frameW, frameH := style.GetFrameSize()

	return style.
		Width(c.width - frameW).
		Height(c.height - frameH).
		Render(content)
}

func (c *ListColumn) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.recalcMaxVisible()
	c.ensureVisible() // Scroll to show selected item now that we know the size
}

func (c *ListColumn) Width() int {
	return c.width
}

func (c *ListColumn) Height() int {
	return c.height
}

func (c *ListColumn) SetFocused(focused bool) {
	c.focused = focused
}

func (c *ListColumn) IsFocused() bool {
	return c.focused
}

func (c *ListColumn) Title() string {
	return c.title
}

func (c *ListColumn) SetTitle(title string) {
	c.title = title
}

func (c *ListColumn) SelectedItem() interface{} {
	count := c.ItemCount()
	if count == 0 || c.cursor >= count {
		return nil
	}

	idx := c.mapIndex(c.cursor)
	switch c.columnType {
	case ColumnTypeLibraries:
		return c.libraries[idx]
	case ColumnTypeMovies:
		return c.movies[idx] // Returns *domain.MediaItem
	case ColumnTypeShows:
		return c.shows[idx] // Returns *domain.Show
	case ColumnTypeSeasons:
		return c.seasons[idx] // Returns *domain.Season
	case ColumnTypeEpisodes:
		return c.episodes[idx] // Returns *domain.MediaItem
	default:
		return nil
	}
}

func (c *ListColumn) SelectedIndex() int {
	return c.cursor
}

func (c *ListColumn) SetSelectedIndex(idx int) {
	max := c.ItemCount() - 1
	if max < 0 {
		c.cursor = 0
		return
	}
	if idx < 0 {
		idx = 0
	}
	if idx > max {
		idx = max
	}
	c.cursor = idx
	c.ensureVisible()
}

func (c *ListColumn) ItemCount() int {
	return c.filteredCount()
}

func (c *ListColumn) CanDrillInto() bool {
	item := c.SelectedItem()
	if item == nil {
		return false
	}

	switch c.columnType {
	case ColumnTypeLibraries:
		return true // Can drill into libraries
	case ColumnTypeShows:
		return true // Can drill into shows
	case ColumnTypeSeasons:
		return true // Can drill into seasons
	case ColumnTypeMovies, ColumnTypeEpisodes:
		return false // These are leaf items, play instead
	default:
		return false
	}
}

func (c *ListColumn) IsEmpty() bool {
	return c.ItemCount() == 0
}

func (c *ListColumn) SetLoading(loading bool) {
	c.loading = loading
}

func (c *ListColumn) IsLoading() bool {
	return c.loading
}

func (c *ListColumn) SetItems(items interface{}) {
	c.loading = false
	c.cursor = 0
	c.offset = 0
	c.clearFilter()
	c.sortedIdx = nil

	switch v := items.(type) {
	case []domain.Library:
		c.libraries = v
		c.columnType = ColumnTypeLibraries
	case []*domain.MediaItem:
		// Could be movies or episodes - check type if available
		if len(v) > 0 && v[0].Type == domain.MediaTypeEpisode {
			c.episodes = v
			c.columnType = ColumnTypeEpisodes
		} else {
			c.movies = v
			c.columnType = ColumnTypeMovies
		}
	case []*domain.Show:
		c.shows = v
		c.columnType = ColumnTypeShows
	case []*domain.Season:
		c.seasons = v
		c.columnType = ColumnTypeSeasons
	}

	// Re-apply current sort if active
	if c.sortField != SortDefault {
		c.buildSortedIdx()
	}
}

// Additional methods

// ColumnType returns the column's content type
func (c *ListColumn) ColumnType() ColumnType {
	return c.columnType
}

// SetLibraryStates updates the library sync states (for library column)
func (c *ListColumn) SetLibraryStates(states map[string]LibrarySyncState) {
	c.libraryStates = states
}

// SetSpinnerFrame updates the spinner animation frame
func (c *ListColumn) SetSpinnerFrame(frame int) {
	c.spinnerFrame = frame
}

// SelectedLibrary returns the selected library (if in library column)
func (c *ListColumn) SelectedLibrary() *domain.Library {
	if c.columnType != ColumnTypeLibraries {
		return nil
	}
	item := c.SelectedItem()
	if item == nil {
		return nil
	}
	lib := item.(domain.Library)
	return &lib
}

// SelectedShow returns the selected show (if in shows column)
func (c *ListColumn) SelectedShow() *domain.Show {
	if c.columnType != ColumnTypeShows {
		return nil
	}
	item := c.SelectedItem()
	if item == nil {
		return nil
	}
	return item.(*domain.Show) // Already a pointer
}

// SelectedSeason returns the selected season (if in seasons column)
func (c *ListColumn) SelectedSeason() *domain.Season {
	if c.columnType != ColumnTypeSeasons {
		return nil
	}
	item := c.SelectedItem()
	if item == nil {
		return nil
	}
	return item.(*domain.Season) // Already a pointer
}

// SelectedMediaItem returns the selected media item (if in movies/episodes column)
func (c *ListColumn) SelectedMediaItem() *domain.MediaItem {
	switch c.columnType {
	case ColumnTypeMovies, ColumnTypeEpisodes:
		item := c.SelectedItem()
		if item == nil {
			return nil
		}
		return item.(*domain.MediaItem) // Already a pointer
	default:
		return nil
	}
}

// FindIndexByID finds the index of an item by its ID. Returns -1 if not found.
func (c *ListColumn) FindIndexByID(id string) int {
	if id == "" {
		return -1
	}
	switch c.columnType {
	case ColumnTypeLibraries:
		for i, lib := range c.libraries {
			if lib.ID == id {
				return i
			}
		}
	case ColumnTypeMovies:
		for i, m := range c.movies {
			if m.ID == id {
				return i
			}
		}
	case ColumnTypeShows:
		for i, s := range c.shows {
			if s.ID == id {
				return i
			}
		}
	case ColumnTypeSeasons:
		for i, s := range c.seasons {
			if s.ID == id {
				return i
			}
		}
	case ColumnTypeEpisodes:
		for i, e := range c.episodes {
			if e.ID == id {
				return i
			}
		}
	}
	return -1
}

// SetSelectedByID finds an item by ID and selects it. Returns true on success.
func (c *ListColumn) SetSelectedByID(id string) bool {
	if id == "" {
		return true
	}
	idx := c.FindIndexByID(id)
	if idx < 0 {
		return false
	}
	c.SetSelectedIndex(idx)
	return true
}

// ToggleFilter activates the filter input
func (c *ListColumn) ToggleFilter() {
	c.filterActive = true
	c.filterInput.Focus()
	c.recalcMaxVisible()
}

// IsFiltering returns true if filter mode is active
func (c *ListColumn) IsFiltering() bool {
	return c.filterActive
}

// IsFilterTyping returns true if filter is active AND input is focused
func (c *ListColumn) IsFilterTyping() bool {
	return c.filterActive && c.filterInput.Focused()
}

// ClearFilter deactivates the filter and shows all items
func (c *ListColumn) ClearFilter() {
	c.clearFilter()
}

// Internal methods

func (c *ListColumn) recalcMaxVisible() {
	// Interior height = total - border (top+bottom)
	// Reserve space for: title line + scroll indicators (header + footer)
	interiorHeight := c.height - BorderHeight
	c.maxVisible = interiorHeight - ScrollIndicatorLines - 1 // -1 for title
	// Reserve space for filter bar when active
	if c.filterActive {
		c.maxVisible--
	}
	if c.maxVisible < 1 {
		c.maxVisible = 1
	}
}

func (c *ListColumn) ensureVisible() {
	// Don't adjust offset if size hasn't been set yet
	if c.maxVisible <= 0 {
		return
	}
	if c.cursor < c.offset {
		c.offset = c.cursor
	}
	if c.cursor >= c.offset+c.maxVisible {
		c.offset = c.cursor - c.maxVisible + 1
	}
}

func (c *ListColumn) clearFilter() {
	c.filterActive = false
	c.filterQuery = ""
	c.filteredIdx = nil
	c.filterInput.SetValue("")
	c.filterInput.Blur()
	c.recalcMaxVisible()
}

func (c *ListColumn) applyFilter() {
	query := c.filterInput.Value()
	c.filterQuery = query

	if query == "" {
		c.filteredIdx = nil
		return
	}

	// Get titles and do case-insensitive matching
	titles := c.getTitles()
	lowerTitles := make([]string, len(titles))
	for i, t := range titles {
		lowerTitles[i] = strings.ToLower(t)
	}

	matches := fuzzy.Find(strings.ToLower(query), lowerTitles)

	c.filteredIdx = make([]int, len(matches))
	for i, match := range matches {
		c.filteredIdx[i] = match.Index
	}

	// Reset cursor to first match
	c.cursor = 0
	c.offset = 0
}

func (c *ListColumn) getTitles() []string {
	count := c.sortedCount()
	titles := make([]string, count)
	for i := 0; i < count; i++ {
		rawIdx := i
		if c.sortedIdx != nil && i < len(c.sortedIdx) {
			rawIdx = c.sortedIdx[i]
		}
		switch c.columnType {
		case ColumnTypeLibraries:
			titles[i] = c.libraries[rawIdx].Name
		case ColumnTypeMovies:
			titles[i] = c.movies[rawIdx].Title
		case ColumnTypeShows:
			titles[i] = c.shows[rawIdx].Title
		case ColumnTypeSeasons:
			titles[i] = c.seasons[rawIdx].DisplayTitle()
		case ColumnTypeEpisodes:
			titles[i] = c.episodes[rawIdx].Title
		}
	}
	return titles
}

func (c *ListColumn) sortedCount() int {
	if c.sortedIdx != nil {
		return len(c.sortedIdx)
	}
	return c.rawItemCount()
}

func (c *ListColumn) filteredCount() int {
	if c.filteredIdx != nil {
		return len(c.filteredIdx)
	}
	return c.sortedCount()
}

func (c *ListColumn) rawItemCount() int {
	switch c.columnType {
	case ColumnTypeLibraries:
		return len(c.libraries)
	case ColumnTypeMovies:
		return len(c.movies)
	case ColumnTypeShows:
		return len(c.shows)
	case ColumnTypeSeasons:
		return len(c.seasons)
	case ColumnTypeEpisodes:
		return len(c.episodes)
	default:
		return 0
	}
}

func (c *ListColumn) mapIndex(i int) int {
	idx := i
	if c.filteredIdx != nil && idx < len(c.filteredIdx) {
		idx = c.filteredIdx[idx]
	}
	if c.sortedIdx != nil && idx < len(c.sortedIdx) {
		return c.sortedIdx[idx]
	}
	return idx
}

// Rendering

func (c *ListColumn) renderContent() string {
	// Content width = column width - border (2 chars for left+right border)
	itemWidth := c.width - BorderWidth
	if itemWidth < 10 {
		itemWidth = 10
	}

	// Title line (styled, truncated to fit column width)
	titleLine := styles.AccentStyle.Render(styles.Truncate(c.title, itemWidth))

	// Loading state
	if c.loading {
		spinner := listColumnSpinnerFrames[c.spinnerFrame%len(listColumnSpinnerFrames)]
		loadingLine := styles.DimStyle.Render(spinner + " Loading...")
		return titleLine + "\n" + " " + "\n" + loadingLine + "\n" + " "
	}

	count := c.ItemCount()
	if count == 0 {
		emptyMsg := styles.DimStyle.Render("No items")
		if c.filterActive && c.filterQuery != "" {
			emptyMsg = styles.DimStyle.Render("No matches")
		}
		return titleLine + "\n" + " " + "\n" + emptyMsg + "\n" + " "
	}

	var lines []string

	end := c.offset + c.maxVisible
	if end > count {
		end = count
	}

	for i := c.offset; i < end; i++ {
		selected := i == c.cursor
		idx := c.mapIndex(i)
		var line string

		switch c.columnType {
		case ColumnTypeLibraries:
			line = c.renderLibraryItem(c.libraries[idx], selected, itemWidth)
		case ColumnTypeMovies:
			line = c.renderMovieItem(*c.movies[idx], selected, itemWidth)
		case ColumnTypeShows:
			line = c.renderShowItem(*c.shows[idx], selected, itemWidth)
		case ColumnTypeSeasons:
			line = c.renderSeasonItem(*c.seasons[idx], selected, itemWidth)
		case ColumnTypeEpisodes:
			line = c.renderEpisodeItem(*c.episodes[idx], selected, itemWidth)
		}

		lines = append(lines, line)
	}

	// ALWAYS reserve space for header (even if empty) to prevent layout shifts
	header := " "
	if c.offset > 0 {
		header = styles.DimStyle.Render("↑ more")
	}

	// ALWAYS reserve space for footer (even if empty)
	footer := " "
	if end < count {
		footer = styles.DimStyle.Render("↓ more")
	}

	content := strings.Join(lines, "\n")
	content = titleLine + "\n" + header + "\n" + content + "\n" + footer

	// Add filter bar at bottom if active
	if c.filterActive {
		content += "\n" + c.renderFilterBar(itemWidth)
	}

	return content
}

func (c *ListColumn) renderLibraryItem(lib domain.Library, selected bool, width int) string {
	// Get sync state for this library
	state := c.libraryStates[lib.ID]

	var prefix string
	var prefixFg lipgloss.Color

	switch state.Status {
	case StatusSyncing:
		spinner := listColumnSpinnerFrames[c.spinnerFrame%len(listColumnSpinnerFrames)]
		prefix = spinner + " "
		prefixFg = styles.PlexOrange
	case StatusSynced:
		prefix = "✓ "
		prefixFg = styles.Green
	case StatusError:
		prefix = "✗ "
		prefixFg = styles.Red
	default:
		prefix = "  "
		prefixFg = styles.DimGray
	}

	title := lib.Name
	// Show count if available
	if state.Status == StatusSyncing && state.Total > 0 {
		title = fmt.Sprintf("%s (%d/%d)", lib.Name, state.Loaded, state.Total)
	} else if state.Status == StatusSynced && state.Loaded > 0 {
		title = fmt.Sprintf("%s (%d)", lib.Name, state.Loaded)
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

func (c *ListColumn) renderShowItem(show domain.Show, selected bool, width int) string {
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

func (c *ListColumn) renderSeasonItem(season domain.Season, selected bool, width int) string {
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
	plexOrange := styles.PlexOrange

	// Available space: width - indicator(1) - space(1) - code - space(1) - margins(2)
	availableForTitle := width - 4 - len(code) - 1
	if availableForTitle < 5 {
		availableForTitle = 5
	}
	title := styles.Truncate(item.Title, availableForTitle)

	parts := []styles.RowPart{
		{Text: indicatorChar, Foreground: &indicatorFg},
		{Text: " " + code, Foreground: &plexOrange},
		{Text: " " + title, Foreground: nil},
	}

	return styles.RenderListRow(parts, selected, width)
}

func (c *ListColumn) renderFilterBar(width int) string {
	input := c.filterInput.View()
	count := c.ItemCount()
	total := c.rawItemCount()

	// Show match count
	countStr := ""
	if c.filterQuery != "" {
		countStr = styles.DimStyle.Render(fmt.Sprintf(" [%d/%d]", count, total))
	}

	return input + countStr
}

// Sort methods

// ApplySort sets the sort field and direction, rebuilds sortedIdx, and resets view
func (c *ListColumn) ApplySort(field SortField, dir SortDirection) {
	c.sortField = field
	c.sortDir = dir
	c.clearFilter()
	c.cursor = 0
	c.offset = 0

	if field == SortDefault {
		c.sortedIdx = nil
		return
	}

	c.buildSortedIdx()
}

// SortState returns the current sort field and direction
func (c *ListColumn) SortState() (SortField, SortDirection) {
	return c.sortField, c.sortDir
}

// buildSortedIdx builds the sortedIdx mapping based on current sortField/sortDir
func (c *ListColumn) buildSortedIdx() {
	n := c.rawItemCount()
	if n == 0 {
		c.sortedIdx = nil
		return
	}

	c.sortedIdx = make([]int, n)
	for i := range c.sortedIdx {
		c.sortedIdx[i] = i
	}

	sort.SliceStable(c.sortedIdx, func(a, b int) bool {
		ia, ib := c.sortedIdx[a], c.sortedIdx[b]
		cmp := c.compareBySortField(ia, ib)
		if c.sortDir == SortDesc {
			return cmp > 0
		}
		return cmp < 0
	})
}

// compareBySortField compares two items by the current sort field.
// Returns negative if i < j, 0 if equal, positive if i > j.
func (c *ListColumn) compareBySortField(i, j int) int {
	switch c.sortField {
	case SortTitle:
		return c.compareTitles(i, j)
	case SortDateAdded:
		return c.compareAddedAt(i, j)
	case SortLastUpdated:
		return c.compareUpdatedAt(i, j)
	case SortReleased:
		return c.compareYear(i, j)
	default:
		return 0
	}
}

func (c *ListColumn) compareTitles(i, j int) int {
	ti := c.getSortTitle(i)
	tj := c.getSortTitle(j)
	if ti < tj {
		return -1
	}
	if ti > tj {
		return 1
	}
	return 0
}

func (c *ListColumn) getSortTitle(idx int) string {
	switch c.columnType {
	case ColumnTypeMovies:
		if c.movies[idx].SortTitle != "" {
			return strings.ToLower(c.movies[idx].SortTitle)
		}
		return strings.ToLower(c.movies[idx].Title)
	case ColumnTypeShows:
		if c.shows[idx].SortTitle != "" {
			return strings.ToLower(c.shows[idx].SortTitle)
		}
		return strings.ToLower(c.shows[idx].Title)
	default:
		return ""
	}
}

func (c *ListColumn) compareAddedAt(i, j int) int {
	ai, aj := c.getAddedAt(i), c.getAddedAt(j)
	if ai < aj {
		return -1
	}
	if ai > aj {
		return 1
	}
	return 0
}

func (c *ListColumn) getAddedAt(idx int) int64 {
	switch c.columnType {
	case ColumnTypeMovies:
		return c.movies[idx].AddedAt
	case ColumnTypeShows:
		return c.shows[idx].AddedAt
	default:
		return 0
	}
}

func (c *ListColumn) compareUpdatedAt(i, j int) int {
	ai, aj := c.getUpdatedAt(i), c.getUpdatedAt(j)
	if ai < aj {
		return -1
	}
	if ai > aj {
		return 1
	}
	return 0
}

func (c *ListColumn) getUpdatedAt(idx int) int64 {
	switch c.columnType {
	case ColumnTypeMovies:
		return c.movies[idx].UpdatedAt
	case ColumnTypeShows:
		return c.shows[idx].UpdatedAt
	default:
		return 0
	}
}

func (c *ListColumn) compareYear(i, j int) int {
	yi, yj := c.getYear(i), c.getYear(j)
	if yi < yj {
		return -1
	}
	if yi > yj {
		return 1
	}
	return 0
}

func (c *ListColumn) getYear(idx int) int {
	switch c.columnType {
	case ColumnTypeMovies:
		return c.movies[idx].Year
	case ColumnTypeShows:
		return c.shows[idx].Year
	default:
		return 0
	}
}
