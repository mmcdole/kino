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

// GlobalSearch is the fuzzy search modal component
type GlobalSearch struct {
	input     textinput.Model
	results   []service.FilterResult
	cursor    int
	visible   bool
	width     int
	height    int
	loading   bool
	prevQuery string
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
	o.visible = true
	o.input.Focus()
	o.input.SetValue("")
	o.input.Placeholder = "Type to search..."
	o.input.Prompt = "🔍 "
	o.results = nil
	o.cursor = 0
	o.loading = false
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

// SetResults sets the search results with match highlighting data
func (o *GlobalSearch) SetResults(results []service.FilterResult) {
	o.results = results
	o.cursor = 0
	o.loading = false
}

// SetSize updates the component dimensions
func (o *GlobalSearch) SetSize(width, height int) {
	o.width = width
	o.height = height
	o.input.Width = width - 10
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
func (o GlobalSearch) Selected() *service.FilterItem {
	if len(o.results) == 0 || o.cursor >= len(o.results) {
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
			if resultCount > 0 {
				return o, nil, true // Selected
			}
			return o, nil, false

		case key.Matches(msg, GlobalSearchKeys.Down):
			if o.cursor < resultCount-1 {
				o.cursor++
			}
			return o, nil, false

		case key.Matches(msg, GlobalSearchKeys.Up):
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
func (o GlobalSearch) View() string {
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

	// Title
	b.WriteString("Global Search")
	b.WriteString("\n\n")

	// Input field
	b.WriteString(o.input.View())
	b.WriteString("\n\n")

	// Results
	if o.loading {
		b.WriteString(styles.SpinnerStyle.Render("Searching..."))
	} else {
		o.renderResults(&b, modalWidth, maxResults)
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

// renderResults renders the search results
func (o GlobalSearch) renderResults(b *strings.Builder, modalWidth, maxResults int) {
	if len(o.results) == 0 && o.input.Value() != "" {
		b.WriteString(styles.DimStyle.Render("No matches found"))
		return
	}
	if len(o.results) == 0 {
		// Don't show anything when empty - placeholder already guides the user
		return
	}

	displayCount := len(o.results)
	if displayCount > maxResults {
		displayCount = maxResults
	}

	for i := 0; i < displayCount; i++ {
		result := o.results[i]
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

	if len(o.results) > maxResults {
		b.WriteString(styles.DimStyle.Render(fmt.Sprintf("... and %d more", len(o.results)-maxResults)))
	}
}
