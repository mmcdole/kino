package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/service"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// Omnibar is the fuzzy search modal component
type Omnibar struct {
	input         textinput.Model
	results       []domain.MediaItem
	filterResults []service.FilterResult // Changed to FilterResult for match highlighting
	filterMode    bool
	cursor        int
	visible       bool
	width         int
	height        int
	loading       bool
	prevQuery     string // Track query changes for real-time filtering
}

// NewOmnibar creates a new omnibar component
func NewOmnibar() Omnibar {
	ti := textinput.New()
	ti.Placeholder = "Search..."
	ti.CharLimit = 100
	ti.Width = 40
	ti.Prompt = "/ "
	ti.PromptStyle = styles.AccentStyle
	ti.TextStyle = lipgloss.NewStyle().Foreground(styles.White)
	ti.PlaceholderStyle = styles.DimStyle

	return Omnibar{
		input: ti,
	}
}

// Show makes the omnibar visible and focuses the input
func (o *Omnibar) Show() {
	o.visible = true
	o.filterMode = false
	o.input.Focus()
	o.input.SetValue("")
	o.input.Placeholder = "Search..."
	o.results = nil
	o.filterResults = nil
	o.cursor = 0
	o.loading = false
	o.prevQuery = ""
}

// ShowFilterMode makes the omnibar visible in filter mode
func (o *Omnibar) ShowFilterMode() {
	o.visible = true
	o.filterMode = true
	o.input.Focus()
	o.input.SetValue("")
	o.input.Placeholder = "Type to search..."
	o.input.Prompt = "🔍 "
	o.results = nil
	o.filterResults = nil
	o.cursor = 0
	o.loading = false
	o.prevQuery = ""
}

// Hide hides the omnibar
func (o *Omnibar) Hide() {
	o.visible = false
	o.filterMode = false
	o.input.Blur()
}

// IsVisible returns true if the omnibar is visible
func (o Omnibar) IsVisible() bool {
	return o.visible
}

// IsFilterMode returns true if the omnibar is in filter mode
func (o Omnibar) IsFilterMode() bool {
	return o.filterMode
}

// SetResults sets the search results
func (o *Omnibar) SetResults(results []domain.MediaItem) {
	o.results = results
	o.cursor = 0
	o.loading = false
}

// SetFilterResults sets the filter results with match highlighting data
func (o *Omnibar) SetFilterResults(results []service.FilterResult) {
	o.filterResults = results
	o.cursor = 0
	o.loading = false
}

// SetLoading sets the loading state
func (o *Omnibar) SetLoading(loading bool) {
	o.loading = loading
}

// SetSize updates the component dimensions
func (o *Omnibar) SetSize(width, height int) {
	o.width = width
	o.height = height
	o.input.Width = width - 10
}

// Query returns the current search query
func (o Omnibar) Query() string {
	return o.input.Value()
}

// QueryChanged returns true if the query changed since last check and updates prevQuery
func (o *Omnibar) QueryChanged() bool {
	current := o.input.Value()
	if current != o.prevQuery {
		o.prevQuery = current
		return true
	}
	return false
}

// SelectedResult returns the selected search result
func (o Omnibar) SelectedResult() *domain.MediaItem {
	if len(o.results) == 0 || o.cursor >= len(o.results) {
		return nil
	}
	return &o.results[o.cursor]
}

// SelectedFilterResult returns the selected filter result's FilterItem
func (o Omnibar) SelectedFilterResult() *service.FilterItem {
	if len(o.filterResults) == 0 || o.cursor >= len(o.filterResults) {
		return nil
	}
	return &o.filterResults[o.cursor].FilterItem
}

// ResultCount returns the number of results (filter or search mode)
func (o Omnibar) ResultCount() int {
	if o.filterMode {
		return len(o.filterResults)
	}
	return len(o.results)
}

// Init initializes the component
func (o Omnibar) Init() tea.Cmd {
	return textinput.Blink
}

// Update handles messages
func (o Omnibar) Update(msg tea.Msg) (Omnibar, tea.Cmd, bool) {
	if !o.visible {
		return o, nil, false
	}

	var cmd tea.Cmd
	resultCount := o.ResultCount()

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, OmnibarKeys.Escape):
			o.Hide()
			return o, nil, false

		case key.Matches(msg, OmnibarKeys.Enter):
			if resultCount > 0 {
				return o, nil, true // Selected
			}
			return o, nil, false

		case key.Matches(msg, OmnibarKeys.Down):
			if o.cursor < resultCount-1 {
				o.cursor++
			}
			return o, nil, false

		case key.Matches(msg, OmnibarKeys.Up):
			if o.cursor > 0 {
				o.cursor--
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

// View renders the component
func (o Omnibar) View() string {
	if !o.visible {
		return ""
	}

	// Modal dimensions
	modalWidth := o.width * 2 / 3
	if modalWidth < 40 {
		modalWidth = 40
	}
	if modalWidth > 80 {
		modalWidth = 80
	}
	maxResults := 10

	var b strings.Builder

	// Title (no background styling - let the modal handle it)
	if o.filterMode {
		b.WriteString("Global Search")
		b.WriteString("\n\n")
	}

	// Input field
	b.WriteString(o.input.View())
	b.WriteString("\n\n")

	// Results - handle both filter mode and search mode
	if o.loading {
		b.WriteString(styles.SpinnerStyle.Render("Searching..."))
	} else if o.filterMode {
		o.renderFilterResults(&b, modalWidth, maxResults)
	} else {
		o.renderSearchResults(&b, modalWidth, maxResults)
	}

	// Center the modal
	content := lipgloss.NewStyle().
		Width(modalWidth - 4).
		Render(b.String())

	modal := styles.ModalStyle.
		Width(modalWidth).
		Render(content)

	// Center horizontally and vertically
	return lipgloss.Place(
		o.width,
		o.height,
		lipgloss.Center,
		lipgloss.Center,
		modal,
	)
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
		reset     = "\033[0m"
		orange    = "\033[38;5;208m" // PlexOrange approximate
		orangeBold = "\033[38;5;208;1m"
		gray      = "\033[38;5;250m" // LightGray approximate
		white     = "\033[38;5;255m"
		bgSlate   = "\033[48;5;238m" // SlateLight approximate
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

// renderFilterResults renders the filter mode results
func (o Omnibar) renderFilterResults(b *strings.Builder, modalWidth, maxResults int) {
	if len(o.filterResults) == 0 && o.input.Value() != "" {
		b.WriteString(styles.DimStyle.Render("No matches found"))
		return
	}
	if len(o.filterResults) == 0 {
		// Don't show anything when empty - placeholder already guides the user
		return
	}

	displayCount := len(o.filterResults)
	if displayCount > maxResults {
		displayCount = maxResults
	}

	for i := 0; i < displayCount; i++ {
		result := o.filterResults[i]
		selected := i == o.cursor

		var line strings.Builder

		// Type badge with library context
		switch result.Type {
		case domain.MediaTypeMovie:
			line.WriteString(styles.DimBadgeStyle.Render("MOV"))
		case domain.MediaTypeShow:
			line.WriteString(styles.DimBadgeStyle.Render("SHOW"))
		case domain.MediaTypeEpisode:
			line.WriteString(styles.DimBadgeStyle.Render("EP"))
		}
		line.WriteString(" ")

		// Library context (dimmed)
		if result.NavContext.LibraryName != "" {
			line.WriteString(styles.DimStyle.Render(result.NavContext.LibraryName))
			line.WriteString(styles.DimStyle.Render(" > "))
		}

		// Build display title
		title := result.Title
		matchedIndexes := result.MatchedIndexes
		maxTitleWidth := modalWidth - 25
		if result.Type == domain.MediaTypeEpisode {
			// For episodes, show: ShowTitle - S01E01 Title
			if item, ok := result.Item.(*domain.MediaItem); ok {
				title = fmt.Sprintf("%s - %s %s", item.ShowTitle, item.EpisodeCode(), item.Title)
				// Reset matched indexes since the title format changed
				matchedIndexes = nil
			}
		} else if result.Type == domain.MediaTypeMovie {
			// For movies, show: Title (Year)
			if item, ok := result.Item.(*domain.MediaItem); ok && item.Year > 0 {
				title = fmt.Sprintf("%s (%d)", item.Title, item.Year)
				// Matched indexes still apply to the title portion
			}
		}
		title = styles.Truncate(title, maxTitleWidth)

		// Apply highlighting to the title
		line.WriteString(highlightMatches(title, matchedIndexes, selected))

		b.WriteString(line.String())
		b.WriteString("\n")
	}

	if len(o.filterResults) > maxResults {
		b.WriteString(styles.DimStyle.Render(fmt.Sprintf("... and %d more", len(o.filterResults)-maxResults)))
	}
}

// renderSearchResults renders the search mode results (original behavior)
func (o Omnibar) renderSearchResults(b *strings.Builder, modalWidth, maxResults int) {
	if len(o.results) == 0 && o.input.Value() != "" {
		b.WriteString(styles.DimStyle.Render("No results"))
		return
	}

	displayCount := len(o.results)
	if displayCount > maxResults {
		displayCount = maxResults
	}

	for i := 0; i < displayCount; i++ {
		result := o.results[i]
		selected := i == o.cursor

		// Build result line
		var line strings.Builder

		// Watch status indicator
		indicator := styles.RenderWatchStatus(result.IsPlayed, int64(result.ViewOffset.Milliseconds()))
		line.WriteString(indicator)
		line.WriteString(" ")

		// Type badge
		switch result.Type {
		case domain.MediaTypeMovie:
			line.WriteString(styles.DimBadgeStyle.Render("MOV"))
		case domain.MediaTypeEpisode:
			line.WriteString(styles.DimBadgeStyle.Render("EP"))
		}
		line.WriteString(" ")

		// Title
		title := result.Title
		if result.Type == domain.MediaTypeEpisode {
			title = result.ShowTitle + " - " + result.EpisodeCode() + " " + result.Title
		}
		if result.Year > 0 && result.Type == domain.MediaTypeMovie {
			title = title + " (" + intToString(result.Year) + ")"
		}
		title = styles.Truncate(title, modalWidth-15)

		style := styles.NormalItemStyle
		if selected {
			style = styles.SelectedItemStyle
		}
		line.WriteString(style.Render(title))

		b.WriteString(line.String())
		b.WriteString("\n")
	}

	if len(o.results) > maxResults {
		b.WriteString(styles.DimStyle.Render("... and " + intToString(len(o.results)-maxResults) + " more"))
	}
}

// intToString converts int to string without importing strconv
func intToString(n int) string {
	if n == 0 {
		return "0"
	}

	negative := n < 0
	if negative {
		n = -n
	}

	var digits []byte
	for n > 0 {
		digits = append([]byte{byte('0' + n%10)}, digits...)
		n /= 10
	}

	if negative {
		digits = append([]byte{'-'}, digits...)
	}

	return string(digits)
}
