package components

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/mmcdole/kino/internal/domain"
	"github.com/mmcdole/kino/internal/tui/styles"
)

// Layout constants for list columns
const (
	// Border adds 1 char on each side (left+right for width, top+bottom for height)
	BorderWidth  = 2
	BorderHeight = 2

	// Scroll indicators ("↑ more" and "↓ more") each take 1 line
	ScrollIndicatorLines = 2
)

// ColumnOptions describe what a column offers. The zero value is a list in
// the server's natural order.
type ColumnOptions struct {
	SortFields  []SortField // sorts the user can choose; none means natural order only
	DefaultSort SortField
	ShowParent  bool // name each episode's show, for lists that mix shows
}

// ListColumn is a scrollable, filterable list of collection items.
type ListColumn struct {
	view       listView
	opts       ColumnOptions
	hasContent bool

	width, height int
	focused       bool
	title         string

	feedback      CollectionFeedback
	spinnerFrame  int
	libraryStates map[string]CollectionFeedback // library rows only

	filterActive bool
	filterInput  textinput.Model

	showWatchStatus   bool
	showLibraryCounts bool

	// contentID names the collection this column shows
	contentID string
}

// NewListColumn creates an empty column
func NewListColumn(title string, opts ColumnOptions) *ListColumn {
	ti := textinput.New()
	ti.Placeholder = "type to filter..."
	ti.Prompt = "/ "
	ti.PromptStyle = styles.FilterPromptStyle
	ti.TextStyle = styles.FilterStyle

	return &ListColumn{
		view:          listView{sortField: opts.DefaultSort},
		opts:          opts,
		title:         title,
		filterInput:   ti,
		libraryStates: make(map[string]CollectionFeedback),
	}
}

func (c *ListColumn) Update(msg tea.Msg) (*ListColumn, tea.Cmd) {
	keyMsg, ok := msg.(tea.KeyMsg)
	if !c.focused || !ok {
		return c, nil
	}

	// Typing into the filter
	if c.IsFilterTyping() {
		switch {
		case key.Matches(keyMsg, ListColumnKeys.Escape):
			c.clearFilter()
			return c, nil
		case key.Matches(keyMsg, ListColumnKeys.Enter):
			// Accept filter, blur input to allow navigation
			c.filterInput.Blur()
			return c, nil
		case keyMsg.String() == "backspace" && c.filterInput.Value() == "":
			c.clearFilter()
			return c, nil
		}
		var cmd tea.Cmd
		c.filterInput, cmd = c.filterInput.Update(msg)
		c.view.filter(c.filterInput.Value())
		return c, cmd
	}

	// Navigating filtered results
	if c.filterActive {
		switch {
		case key.Matches(keyMsg, ListColumnKeys.Escape):
			c.clearFilter()
			return c, nil
		case key.Matches(keyMsg, ListColumnKeys.Filter):
			c.filterInput.Focus()
			return c, nil
		}
	}

	page := c.view.rows
	switch {
	case key.Matches(keyMsg, ListColumnKeys.Down):
		c.view.move(1)
	case key.Matches(keyMsg, ListColumnKeys.Up):
		c.view.move(-1)
	case key.Matches(keyMsg, ListColumnKeys.Home):
		c.view.moveTo(0)
	case key.Matches(keyMsg, ListColumnKeys.End):
		c.view.moveTo(c.view.len() - 1)
	case key.Matches(keyMsg, ListColumnKeys.HalfDown):
		c.view.move(page / 2)
	case key.Matches(keyMsg, ListColumnKeys.HalfUp):
		c.view.move(-page / 2)
	case key.Matches(keyMsg, ListColumnKeys.PageDown):
		c.view.move(page)
	case key.Matches(keyMsg, ListColumnKeys.PageUp):
		c.view.move(-page)
	}
	return c, nil
}

func (c *ListColumn) View() string {
	style := styles.InactiveBorder
	if c.focused {
		style = styles.ActiveBorder
	}
	// Subtract frame (border) size so total rendered size equals c.width x c.height
	frameW, frameH := style.GetFrameSize()
	return style.
		Width(c.width - frameW).
		Height(c.height - frameH).
		Render(c.renderContent())
}

func (c *ListColumn) SetSize(width, height int) {
	c.width = width
	c.height = height
	c.resizeRows()
}

func (c *ListColumn) SetFocused(focused bool) { c.focused = focused }
func (c *ListColumn) Title() string           { return c.title }

// SelectedItem returns the item under the cursor, or nil.
func (c *ListColumn) SelectedItem() domain.ListItem { return c.view.selected() }
func (c *ListColumn) SelectedIndex() int            { return c.view.cursor }
func (c *ListColumn) SetSelectedIndex(pos int)      { c.view.moveTo(pos) }
func (c *ListColumn) ItemCount() int                { return c.view.len() }

func (c *ListColumn) CanDrillInto() bool {
	item := c.SelectedItem()
	return item != nil && present(item).DrillDown
}

// SetFeedback receives the same presentation value used by library rows and the inspector.
func (c *ListColumn) SetFeedback(feedback CollectionFeedback) { c.feedback = feedback }
func (c *ListColumn) IsLoading() bool                         { return c.feedback.Pending && !c.hasContent }
func (c *ListColumn) IsRefreshing() bool                      { return c.feedback.Pending && c.hasContent }
func (c *ListColumn) HasLoadFailed() bool                     { return !c.feedback.Pending && c.feedback.Error != nil }
func (c *ListColumn) HasContent() bool                        { return c.hasContent }

// SetItems shows items as a fresh list: default sort, no filter, cursor at the top.
func (c *ListColumn) SetItems(items []domain.ListItem) {
	c.hasContent = true
	c.clearFilter()
	c.view = listView{rows: c.view.rows, sortField: c.opts.DefaultSort}
	c.view.setItems(items)
}

// ReplaceItems swaps the column's content while preserving the user's view
// state: the selected item, sort, and filter all survive. On a column with no
// prior content it behaves like SetItems.
func (c *ListColumn) ReplaceItems(items []domain.ListItem) {
	if !c.hasContent {
		c.SetItems(items)
		return
	}
	c.view.setItems(items)
}

// SortFields returns the sorts this column offers; nil means none.
func (c *ListColumn) SortFields() []SortField { return c.opts.SortFields }

// ApplySort orders the column. An active filter survives the re-sort.
func (c *ListColumn) ApplySort(field SortField, dir SortDirection) { c.view.sortBy(field, dir) }

// SortState returns the current sort field and direction
func (c *ListColumn) SortState() (SortField, SortDirection) { return c.view.sortField, c.view.sortDir }

// SetLibraryStates updates the library summaries and activity (for library rows)
func (c *ListColumn) SetLibraryStates(states map[string]CollectionFeedback) {
	c.libraryStates = states
}

func (c *ListColumn) SetSpinnerFrame(frame int)    { c.spinnerFrame = frame }
func (c *ListColumn) SetShowWatchStatus(show bool) { c.showWatchStatus = show }

// SetShowLibraryCounts sets whether to keep library item counts visible after sync
func (c *ListColumn) SetShowLibraryCounts(show bool) { c.showLibraryCounts = show }

func (c *ListColumn) SetContentID(id string) { c.contentID = id }
func (c *ListColumn) ContentID() string      { return c.contentID }

// SelectedLibrary returns the selected library, if a library is selected.
func (c *ListColumn) SelectedLibrary() *domain.Library {
	lib, _ := c.SelectedItem().(*domain.Library)
	return lib
}

// SelectedMediaItem returns the selected movie or episode, if one is selected.
func (c *ListColumn) SelectedMediaItem() *domain.MediaItem {
	item, _ := c.SelectedItem().(*domain.MediaItem)
	return item
}

// SelectedPlaylist returns the selected playlist, if one is selected.
func (c *ListColumn) SelectedPlaylist() *domain.Playlist {
	playlist, _ := c.SelectedItem().(*domain.Playlist)
	return playlist
}

// SetSelectedByID selects the item with id. An empty id always succeeds.
func (c *ListColumn) SetSelectedByID(id string) bool {
	return id == "" || c.view.selectID(id)
}

// ToggleFilter activates the filter input
func (c *ListColumn) ToggleFilter() {
	c.filterActive = true
	c.filterInput.Focus()
	c.resizeRows()
}

func (c *ListColumn) IsFiltering() bool    { return c.filterActive }
func (c *ListColumn) IsFilterTyping() bool { return c.filterActive && c.filterInput.Focused() }

// ClearFilter deactivates the filter and shows all items
func (c *ListColumn) ClearFilter() { c.clearFilter() }

func (c *ListColumn) clearFilter() {
	c.filterActive = false
	c.filterInput.SetValue("")
	c.filterInput.Blur()
	c.view.filter("")
	c.resizeRows()
}

// resizeRows fits the list between the title, scroll indicators and filter bar.
func (c *ListColumn) resizeRows() {
	rows := c.height - BorderHeight - ScrollIndicatorLines - 1 // -1 for title
	if c.filterActive {
		rows--
	}
	c.view.setRows(rows)
}

func (c *ListColumn) renderContent() string {
	// Content width = column width - border (2 chars for left+right border)
	itemWidth := max(c.width-BorderWidth, 10)

	// Title line; background refreshes show a spinner next to the title
	// while items stay visible
	title := "  " + c.title
	if c.IsRefreshing() && c.feedback.Activity.Visible {
		title = styles.SpinnerFrames[c.spinnerFrame%len(styles.SpinnerFrames)] + " " + c.title
	}
	titleLine := styles.AccentStyle.Render(styles.Truncate(title, itemWidth))

	if c.IsLoading() {
		spinner := styles.SpinnerFrames[c.spinnerFrame%len(styles.SpinnerFrames)]
		loadingLine := " "
		if c.feedback.Activity.Visible {
			loadingLine = styles.DimStyle.Render(spinner + " Loading...")
		}
		return titleLine + "\n" + " " + "\n" + loadingLine + "\n" + " "
	}

	// Failed load: actionable dead-end instead of an infinite spinner
	if c.HasLoadFailed() && !c.hasContent {
		failedLine := styles.ErrorStyle.Render("✗ Failed to load")
		retryLine := styles.DimStyle.Render("press r to retry")
		return titleLine + "\n" + " " + "\n" + failedLine + "\n" + retryLine
	}

	count := c.view.len()
	if count == 0 {
		emptyMsg := styles.DimStyle.Render("No items")
		if c.filterActive && c.view.query != "" {
			emptyMsg = styles.DimStyle.Render("No matches")
		}
		header, footer := " ", " "
		if c.HasLoadFailed() {
			header = styles.ErrorStyle.Render("Refresh failed")
			footer = styles.DimStyle.Render("press r to retry")
		}
		content := titleLine + "\n" + header + "\n" + emptyMsg + "\n" + footer
		// Show the filter bar so the user can see what they're typing
		if c.filterActive {
			content += "\n" + c.renderFilterBar()
		}
		return content
	}

	end := min(c.view.offset+c.view.rows, count)
	lines := make([]string, 0, end-c.view.offset)
	for pos := c.view.offset; pos < end; pos++ {
		lines = append(lines, c.renderRow(c.view.at(pos), pos == c.view.cursor, itemWidth))
	}

	// Header and footer lines are always reserved to prevent layout shifts
	header := " "
	if c.HasLoadFailed() {
		header = styles.ErrorStyle.Render(styles.Truncate("Refresh failed · r to retry", itemWidth))
	} else if c.view.offset > 0 {
		header = styles.DimStyle.Render("↑ more")
	}
	footer := " "
	if end < count {
		footer = styles.DimStyle.Render("↓ more")
	}

	content := titleLine + "\n" + header + "\n" + strings.Join(lines, "\n") + "\n" + footer
	if c.filterActive {
		content += "\n" + c.renderFilterBar()
	}
	return content
}

func (c *ListColumn) renderFilterBar() string {
	input := c.filterInput.View()
	if c.view.query == "" {
		return input
	}
	return input + styles.DimStyle.Render(fmt.Sprintf(" [%d/%d]", c.view.len(), len(c.view.items)))
}
