package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/search"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// GlobalSearch is the fuzzy search modal component
type GlobalSearch struct {
	loading        bool
	loadingVisible bool
	resultsQuery   string
	input          textinput.Model
	results        []search.FilterResult
	cursor         int
	offset         int
	visible        bool
	width          int
	height         int
	prevQuery      string
}

// NewGlobalSearch creates a new global search component
func NewGlobalSearch() GlobalSearch {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 100
	ti.Width = 40
	ti.Prompt = "/ "
	ti.PromptStyle = styles.AccentStyle
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.White)
	ti.PlaceholderStyle = styles.DimStyle

	return GlobalSearch{
		input: ti,
	}
}

// Show makes the global search visible and focuses the input
func (o *GlobalSearch) Show() {
	o.loading = false
	o.loadingVisible = false
	o.resultsQuery = ""
	o.visible = true
	o.input.Focus()
	o.input.SetValue("")
	o.input.Placeholder = "Type to search..."
	o.input.Prompt = "🔍 "
	o.SetSize(o.width, o.height)
	o.results = nil
	o.cursor = 0
	o.offset = 0
	o.prevQuery = ""
}

// Hide hides the global search
func (o *GlobalSearch) Hide() {
	o.visible = false
	o.input.Blur()
}

// IsVisible returns true if the global search is visible
func (o GlobalSearch) IsVisible() bool {
	return o.visible
}

// SetResults replaces the results, preserving selection when the same query is reindexed.
func (o *GlobalSearch) SetResults(results []search.FilterResult) {
	var selected *search.FilterItem
	if o.resultsQuery == o.Query() && o.cursor < len(o.results) {
		selected = &o.results[o.cursor].FilterItem
	}
	o.loading = false
	o.loadingVisible = false
	o.results = results
	o.resultsQuery = o.Query()
	o.cursor = 0
	if selected != nil {
		for i, result := range results {
			if result.LibraryID == selected.LibraryID && result.Type == selected.Type && result.Item.GetID() == selected.Item.GetID() {
				o.cursor = i
				break
			}
		}
	} else {
		o.offset = 0
	}
	o.ensureVisible(o.layout().rows)
}

// SetLoading retains the displayed results until the pending query completes.
func (o *GlobalSearch) SetLoading(loading bool) {
	o.loading = loading
	o.loadingVisible = false
}

// ShowLoading reveals activity only while a query remains pending.
func (o *GlobalSearch) ShowLoading() {
	o.loadingVisible = o.loading
}

// SetSize fits the input and result viewport to the terminal.
func (o *GlobalSearch) SetSize(width, height int) {
	o.width = max(0, width)
	o.height = max(0, height)
	layout := o.layout()
	o.input.Width = max(1, layout.width-lipgloss.Width(o.input.Prompt)-1)
	o.input.SetCursor(o.input.Position())
	o.ensureVisible(layout.rows)
}

// Query returns the current search query
func (o GlobalSearch) Query() string {
	return o.input.Value()
}

// QueryChanged returns true if the query changed since last check and updates prevQuery
func (o *GlobalSearch) QueryChanged() bool {
	current := o.input.Value()
	if current != o.prevQuery {
		o.prevQuery = current
		return true
	}
	return false
}

// Selected returns the selected result's FilterItem
func (o GlobalSearch) Selected() *search.FilterItem {
	if o.loading || len(o.results) == 0 || o.cursor >= len(o.results) {
		return nil
	}
	return &o.results[o.cursor].FilterItem
}

// ResultCount returns the number of results
func (o GlobalSearch) ResultCount() int {
	return len(o.results)
}

// Init initializes the component
func (o GlobalSearch) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (o GlobalSearch) Update(msg tea.Msg) (GlobalSearch, tea.Cmd, bool) {
	if !o.visible {
		return o, nil, false
	}

	var cmd tea.Cmd
	resultCount := o.ResultCount()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, GlobalSearchKeys.Escape):
			o.Hide()
			return o, nil, false

		case key.Matches(msg, GlobalSearchKeys.Enter):
			if o.Selected() != nil {
				return o, nil, true
			}
			return o, nil, false

		case key.Matches(msg, GlobalSearchKeys.Down):
			if o.cursor < resultCount-1 {
				o.cursor++
				o.ensureVisible(o.layout().rows)
			}
			return o, nil, false

		case key.Matches(msg, GlobalSearchKeys.Up):
			if o.cursor > 0 {
				o.cursor--
				o.ensureVisible(o.layout().rows)
			}
			return o, nil, false

		default:
			// Pass to text input
			o.input, cmd = o.input.Update(msg)
			return o, cmd, false
		}
	}

	// Handle other messages
	o.input, cmd = o.input.Update(msg)
	return o, cmd, false
}

func (o *GlobalSearch) ensureVisible(maxVisible int) {
	maxVisible = max(1, maxVisible)
	o.offset = min(o.offset, max(0, len(o.results)-maxVisible))
	if o.cursor < o.offset {
		o.offset = o.cursor
	}
	if o.cursor >= o.offset+maxVisible {
		o.offset = o.cursor - maxVisible + 1
	}
}

type searchLayout struct {
	width, height int
	header, rows  int
}

// Geometry depends on terminal size so query and result changes cannot move the modal.
func (o GlobalSearch) layout() searchLayout {
	frameWidth, frameHeight := styles.ModalStyle.GetFrameSize()
	width := min(80, max(40, o.width*2/3), max(0, o.width-4))
	height := min(19, max(0, o.height-2))
	layout := searchLayout{width: max(1, width-frameWidth), height: max(1, height-frameHeight), header: 4}
	if layout.height < 7 {
		layout.header = 2
	}
	layout.rows = max(0, layout.height-layout.header-1)
	return layout
}

// View renders the modal; its parent owns placement on the screen.
func (o GlobalSearch) View() string {
	if !o.visible || o.width == 0 || o.height == 0 {
		return ""
	}
	if o.width < 12 || o.height < 9 {
		return ansi.Truncate("Global Search", o.width, "")
	}
	layout := o.layout()
	lines := make([]string, layout.height)
	lines[0] = "Global Search"
	lines[layout.header/2] = o.input.View()
	for row := 0; row < layout.rows && o.offset+row < len(o.results); row++ {
		i := o.offset + row
		lines[layout.header+row] = o.renderResult(o.results[i], i == o.cursor, layout.width)
	}
	if len(o.results) == 0 && o.Query() != "" && !o.loading && layout.rows > 0 {
		lines[layout.header] = styles.DimStyle.Render("No matches found")
	}
	if o.loadingVisible {
		lines[len(lines)-1] = styles.DimStyle.Render("Searching…")
	} else if remaining := len(o.results) - (o.offset + layout.rows); remaining > 0 {
		lines[len(lines)-1] = styles.DimStyle.Render(fmt.Sprintf("... and %d more", remaining))
	}
	for i, line := range lines {
		lines[i] = ansi.Truncate(line, layout.width, "…")
	}
	content := lipgloss.NewStyle().Width(layout.width).Height(layout.height).Render(strings.Join(lines, "\n"))
	return styles.ModalStyle.Render(content)
}

// highlightMatches renders text with matched characters highlighted
// Uses ANSI codes directly to avoid lipgloss padding issues
func highlightMatches(text string, matchedIndexes []int, selected bool) string {
	if len(matchedIndexes) == 0 {
		if selected {
			return styles.SelectedItemStyle.Render(text)
		}
		return styles.NormalItemStyle.Render(text)
	}

	// Create a set of matched indexes for O(1) lookup
	matchSet := make(map[int]bool)
	for _, idx := range matchedIndexes {
		matchSet[idx] = true
	}

	// ANSI escape codes for inline styling (no padding)
	// Orange/bold for matches, gray for normal text
	const (
		reset      = "\033[0m"
		orange     = "\033[38;5;208m" // PlexOrange approximate
		orangeBold = "\033[38;5;208;1m"
		gray       = "\033[38;5;250m" // LightGray approximate
		white      = "\033[38;5;255m"
		bgSlate    = "\033[48;5;238m" // SlateLight approximate
	)

	var matchStart, matchEnd, normalStart, normalEnd string
	if selected {
		// Selected: white bg for normal, orange+bold+bg for match
		normalStart = white + bgSlate
		normalEnd = reset
		matchStart = orangeBold + bgSlate
		matchEnd = reset
	} else {
		// Not selected: gray for normal, orange+bold for match
		normalStart = gray
		normalEnd = reset
		matchStart = orangeBold
		matchEnd = reset
	}

	// Batch consecutive characters with the same style
	var result strings.Builder
	runes := []rune(text)
	i := 0
	for i < len(runes) {
		isMatch := matchSet[i]

		// Collect consecutive characters with the same match state
		var batch strings.Builder
		for i < len(runes) && matchSet[i] == isMatch {
			batch.WriteRune(runes[i])
			i++
		}

		// Render the batch with ANSI codes
		if isMatch {
			result.WriteString(matchStart)
			result.WriteString(batch.String())
			result.WriteString(matchEnd)
		} else {
			result.WriteString(normalStart)
			result.WriteString(batch.String())
			result.WriteString(normalEnd)
		}
	}

	return result.String()
}

func (o GlobalSearch) renderResult(result search.FilterResult, selected bool, width int) string {
	var badge string
	switch result.Type {
	case domain.MediaTypeMovie:
		badge = "MOV"
	case domain.MediaTypeShow:
		badge = "SHOW"
	case domain.MediaTypeEpisode:
		badge = "EP"
	}
	prefix := styles.DimBadgeStyle.Render(badge) + " "
	title := result.Title
	matchedIndexes := result.MatchedIndexes
	if item, ok := result.Item.(*domain.MediaItem); ok {
		switch result.Type {
		case domain.MediaTypeEpisode:
			title = fmt.Sprintf("%s - %s %s", item.ShowTitle, episodeCode(*item), item.Title)
			// Match positions refer to the indexed title, not the episode display label.
			matchedIndexes = nil
		case domain.MediaTypeMovie:
			if item.Year > 0 {
				title = fmt.Sprintf("%s (%d)", item.Title, item.Year)
			}
		}
	}
	title = strings.NewReplacer("\n", " ", "\r", " ", "\t", " ").Replace(title)
	title = styles.Truncate(title, max(1, width-lipgloss.Width(prefix)-2))
	return prefix + highlightMatches(title, matchedIndexes, selected)
}
