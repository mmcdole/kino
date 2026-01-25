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
	// Content - unified storage using ListItem interface
	items []ListItem

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
	sortField SortField
	sortDir   SortDirection
	sortedIdx []int // sorted position → raw index (nil = default order)

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
	col.items = WrapLibraries(libraries)
	return col
}

// NewMoviesColumn creates a column for displaying movies
func NewMoviesColumn(title string, movies []*domain.MediaItem) *ListColumn {
	col := NewListColumn(ColumnTypeMovies, title)
	col.items = WrapMovies(movies)
	return col
}

// NewShowsColumn creates a column for displaying TV shows
func NewShowsColumn(title string, shows []*domain.Show) *ListColumn {
	col := NewListColumn(ColumnTypeShows, title)
	col.items = WrapShows(shows)
	return col
}

// NewSeasonsColumn creates a column for displaying seasons
func NewSeasonsColumn(title string, seasons []*domain.Season) *ListColumn {
	col := NewListColumn(ColumnTypeSeasons, title)
	col.items = WrapSeasons(seasons)
	return col
}

// NewEpisodesColumn creates a column for displaying episodes
func NewEpisodesColumn(title string, episodes []*domain.MediaItem) *ListColumn {
	col := NewListColumn(ColumnTypeEpisodes, title)
	col.items = WrapEpisodes(episodes)
	return col
}

// NewPlaylistsColumn creates a column for displaying playlists
func NewPlaylistsColumn(title string, playlists []*domain.Playlist) *ListColumn {
	col := NewListColumn(ColumnTypePlaylists, title)
	col.items = WrapPlaylists(playlists)
	return col
}

// NewPlaylistItemsColumn creates a column for displaying playlist items
func NewPlaylistItemsColumn(title string, items []*domain.MediaItem) *ListColumn {
	col := NewListColumn(ColumnTypePlaylistItems, title)
	col.items = WrapPlaylistItems(items)
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
	if idx >= len(c.items) {
		return nil
	}
	return c.items[idx].Unwrap()
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
	count := c.ItemCount()
	if count == 0 || c.cursor >= count {
		return false
	}

	idx := c.mapIndex(c.cursor)
	if idx >= len(c.items) {
		return false
	}
	return c.items[idx].CanDrillInto()
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

func (c *ListColumn) SetItems(rawItems interface{}) {
	c.loading = false
	c.cursor = 0
	c.offset = 0
	c.clearFilter()
	c.sortedIdx = nil

	switch v := rawItems.(type) {
	case []domain.Library:
		c.items = WrapLibraries(v)
		c.columnType = ColumnTypeLibraries
	case []*domain.MediaItem:
		// Could be movies, episodes, or playlist items - preserve column type if already set
		if c.columnType == ColumnTypePlaylistItems {
			c.items = WrapPlaylistItems(v)
		} else if len(v) > 0 && v[0].Type == domain.MediaTypeEpisode {
			c.items = WrapEpisodes(v)
			c.columnType = ColumnTypeEpisodes
		} else {
			c.items = WrapMovies(v)
			c.columnType = ColumnTypeMovies
		}
	case []*domain.Show:
		c.items = WrapShows(v)
		c.columnType = ColumnTypeShows
	case []*domain.Season:
		c.items = WrapSeasons(v)
		c.columnType = ColumnTypeSeasons
	case []*domain.Playlist:
		c.items = WrapPlaylists(v)
		c.columnType = ColumnTypePlaylists
	case []ListItem:
		c.items = v
		// columnType should already be set
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
	return item.(*domain.Show)
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
	return item.(*domain.Season)
}

// SelectedMediaItem returns the selected media item (if in movies/episodes/playlist items column)
func (c *ListColumn) SelectedMediaItem() *domain.MediaItem {
	switch c.columnType {
	case ColumnTypeMovies, ColumnTypeEpisodes, ColumnTypePlaylistItems:
		item := c.SelectedItem()
		if item == nil {
			return nil
		}
		return item.(*domain.MediaItem)
	default:
		return nil
	}
}

// SelectedPlaylist returns the selected playlist (if in playlists column)
func (c *ListColumn) SelectedPlaylist() *domain.Playlist {
	if c.columnType != ColumnTypePlaylists {
		return nil
	}
	item := c.SelectedItem()
	if item == nil {
		return nil
	}
	return item.(*domain.Playlist)
}

// FindIndexByID finds the index of an item by its ID. Returns -1 if not found.
func (c *ListColumn) FindIndexByID(id string) int {
	if id == "" {
		return -1
	}
	for i, item := range c.items {
		if item.ItemID() == id {
			return i
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

	// Get filter values from items
	titles := c.getFilterValues()
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

func (c *ListColumn) getFilterValues() []string {
	count := c.sortedCount()
	titles := make([]string, count)
	for i := 0; i < count; i++ {
		rawIdx := i
		if c.sortedIdx != nil && i < len(c.sortedIdx) {
			rawIdx = c.sortedIdx[i]
		}
		if rawIdx < len(c.items) {
			titles[i] = c.items[rawIdx].FilterValue()
		}
	}
	return titles
}

func (c *ListColumn) sortedCount() int {
	if c.sortedIdx != nil {
		return len(c.sortedIdx)
	}
	return len(c.items)
}

func (c *ListColumn) filteredCount() int {
	if c.filteredIdx != nil {
		return len(c.filteredIdx)
	}
	return c.sortedCount()
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
		content := titleLine + "\n" + " " + "\n" + emptyMsg + "\n" + " "
		// Add filter bar if active so user can see what they're typing
		if c.filterActive {
			content += "\n" + c.renderFilterBar(itemWidth)
		}
		return content
	}

	var lines []string

	end := c.offset + c.maxVisible
	if end > count {
		end = count
	}

	for i := c.offset; i < end; i++ {
		selected := i == c.cursor
		idx := c.mapIndex(i)
		line := c.renderItem(idx, selected, itemWidth)
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

// renderItem renders a single item based on column type
func (c *ListColumn) renderItem(idx int, selected bool, width int) string {
	if idx >= len(c.items) {
		return ""
	}

	item := c.items[idx]

	// Dispatch to type-specific renderer based on column type
	// This preserves the existing visual styling for each content type
	switch c.columnType {
	case ColumnTypeLibraries:
		return c.renderLibraryItem(item.(LibraryListItem).Library, selected, width)
	case ColumnTypeMovies:
		return c.renderMovieItem(*item.(MovieListItem).Movie, selected, width)
	case ColumnTypeShows:
		return c.renderShowItem(*item.(ShowListItem).Show, selected, width)
	case ColumnTypeSeasons:
		return c.renderSeasonItem(*item.(SeasonListItem).Season, selected, width)
	case ColumnTypeEpisodes:
		return c.renderEpisodeItem(*item.(EpisodeListItem).Episode, selected, width)
	case ColumnTypePlaylists:
		return c.renderPlaylistItem(*item.(PlaylistListItem).Playlist, selected, width)
	case ColumnTypePlaylistItems:
		return c.renderPlaylistMediaItem(*item.(PlaylistMediaListItem).Item, selected, width)
	default:
		return ""
	}
}

func (c *ListColumn) renderLibraryItem(lib domain.Library, selected bool, width int) string {
	// Special handling for synthetic "Playlists" entry - match library styling
	if lib.Type == "playlist" {
		prefix := "  " // Same spacing as idle libraries
		prefixFg := styles.DimGray
		title := styles.Truncate(lib.Name, width-4)

		parts := []styles.RowPart{
			{Text: prefix, Foreground: &prefixFg},
			{Text: title, Foreground: nil},
		}

		return styles.RenderListRow(parts, selected, width)
	}

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
	total := len(c.items)

	// Show match count
	countStr := ""
	if c.filterQuery != "" {
		countStr = styles.DimStyle.Render(fmt.Sprintf(" [%d/%d]", count, total))
	}

	return input + countStr
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

func (c *ListColumn) renderPlaylistMediaItem(item domain.MediaItem, selected bool, width int) string {
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
	if item.Type == domain.MediaTypeEpisode && item.ShowTitle != "" {
		// Show episode with show context: "Show - S01E05 Title"
		title = fmt.Sprintf("%s - %s %s", item.ShowTitle, item.EpisodeCode(), item.Title)
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
	n := len(c.items)
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
	if i >= len(c.items) || j >= len(c.items) {
		return 0
	}

	itemI := c.items[i]
	itemJ := c.items[j]

	switch c.sortField {
	case SortTitle:
		ti := itemI.SortTitle()
		tj := itemJ.SortTitle()
		if ti < tj {
			return -1
		}
		if ti > tj {
			return 1
		}
		return 0
	case SortDateAdded:
		ai := itemI.SortableAddedAt()
		aj := itemJ.SortableAddedAt()
		if ai < aj {
			return -1
		}
		if ai > aj {
			return 1
		}
		return 0
	case SortLastUpdated:
		ai := itemI.SortableUpdatedAt()
		aj := itemJ.SortableUpdatedAt()
		if ai < aj {
			return -1
		}
		if ai > aj {
			return 1
		}
		return 0
	case SortReleased:
		yi := itemI.SortableYear()
		yj := itemJ.SortableYear()
		if yi < yj {
			return -1
		}
		if yi > yj {
			return 1
		}
		return 0
	default:
		return 0
	}
}
